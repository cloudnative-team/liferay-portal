#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CLOUD_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

readonly CLOUD_DIR

function main {
	_TEMP_DIR=$(mktemp -d)

	trap 'rm -fr "${_TEMP_DIR}"' EXIT

	_write_schemas

	local fail=0
	local pass=0

	_run_test _test_backup_matches_the_liferayinfrastructure_schema
	_run_test _test_cmk_matches_the_liferayinfrastructure_schema
	_run_test _test_common_tags_match_the_liferayinfrastructure_schema
	_run_test _test_database_matches_the_liferayinfrastructure_schema
	_run_test _test_overlay_access_matches_the_liferayinfrastructure_schema
	_run_test _test_search_opensearch_matches_the_liferayinfrastructure_schema
	_run_test _test_search_snapshots_match_the_liferayinfrastructure_schema
	_run_test _test_storage_matches_the_liferayinfrastructure_schema

	echo ""
	echo "Results: ${pass} passed, ${fail} failed."

	if [ ${fail} -eq 0 ]
	then
		return 0
	fi

	return 1
}

function _assert_same_fields {
	local excluded_prefix="${3:-}"
	local key="${1}"
	local values_path="${2}"

	local infrastructure_fields

	infrastructure_fields=$(_list_fields "${_TEMP_DIR}/liferayinfrastructure.json" ".properties.${key}" "${excluded_prefix}")

	local values_fields

	values_fields=$(_list_fields "${CLOUD_DIR}/helm/infrastructure/values.schema.json" ".properties.${values_path}" "${excluded_prefix}")

	if [[ ${infrastructure_fields} == "${values_fields}" ]]
	then
		return 0
	fi

	local field

	for field in $(comm -23 <(echo "${infrastructure_fields}") <(echo "${values_fields}"))
	do
		echo "${values_path//.properties/}.${field} is in the LiferayInfrastructure schema but not in \"values.schema.json\"" >&2
	done

	for field in $(comm -13 <(echo "${infrastructure_fields}") <(echo "${values_fields}"))
	do
		echo "${values_path//.properties/}.${field} is in \"values.schema.json\" but not in the LiferayInfrastructure schema" >&2
	done

	return 1
}

function _list_fields {
	local excluded_prefix="${3}"
	local file="${1}"
	local node="${2}"

	jq \
		--arg excluded_prefix "${excluded_prefix}" \
		--raw-output \
		"${node}"' | [paths(type == "object") | select(length % 2 == 0 and ([.[range(0; length; 2)]] | all(. == "properties"))) | [.[range(1; length; 2)]] | join(".")] | map(select($excluded_prefix == "" or (startswith($excluded_prefix) | not))) | sort[]' \
		"${file}"
}

function _run_test {
	local test_function="${1}"

	local description

	description=$(echo "${test_function}" | sed "s/^_test_//; s/_/ /g")

	if "${test_function}"
	then
		echo "PASS: ${description}"

		((++pass))
	else
		echo "FAIL: ${description}"

		((++fail))
	fi
}

function _test_backup_matches_the_liferayinfrastructure_schema {
	_assert_same_fields backup backup restore
}

function _test_cmk_matches_the_liferayinfrastructure_schema {
	_assert_same_fields cmk cmk
}

function _test_common_tags_match_the_liferayinfrastructure_schema {
	_assert_same_fields commonTags commonTags
}

function _test_database_matches_the_liferayinfrastructure_schema {
	_assert_same_fields database database
}

function _test_overlay_access_matches_the_liferayinfrastructure_schema {
	_assert_same_fields overlayAccess overlayAccess
}

function _test_search_opensearch_matches_the_liferayinfrastructure_schema {
	_assert_same_fields search.properties.opensearch search.properties.opensearch
}

function _test_search_snapshots_match_the_liferayinfrastructure_schema {
	_assert_same_fields search.properties.snapshots search.properties.snapshots
}

function _test_storage_matches_the_liferayinfrastructure_schema {
	_assert_same_fields storage storage
}

function _write_schemas {
	helm template \
		platform-components \
		"${CLOUD_DIR}/helm/platform-components" \
		--show-only templates/crossplane-xrd-liferayinfrastructure.yaml \
		--values "${CLOUD_DIR}/scripts/tests/render-values/platform-components.yaml" \
		| yq --output-format json '.spec.versions[0].schema.openAPIV3Schema.properties.spec' > "${_TEMP_DIR}/liferayinfrastructure.json"
}

main "${@}"