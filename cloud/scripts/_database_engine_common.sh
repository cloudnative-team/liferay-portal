#!/usr/bin/env bash

_SUPPORTED_DATABASE_ENGINES=(
	"postgresql"
)

readonly _SUPPORTED_DATABASE_ENGINES

function assert_database_engine_unchanged {
	local configuration_json_file=${1}

	local environment_configs

	environment_configs=$(_get_custom_resources environmentconfigs.apiextensions.crossplane.io)

	local liferay_infrastructures

	liferay_infrastructures=$(_get_custom_resources liferayinfrastructures.liferay.com)

	local database_engine

	database_engine=$(get_database_engine "${configuration_json_file}")

	local changed_database_engines

	changed_database_engines=$( \
		jq \
			--arg database_engine "${database_engine}" \
			--argjson environment_configs "${environment_configs}" \
			--argjson liferay_infrastructures "${liferay_infrastructures}" \
			--null-input \
			--raw-output \
			'[
				($environment_configs[] | select(.metadata.name == "environment-config") | {database_engine: (.data.setup.database.engine // "postgresql"), name: "deployment"}),
				($liferay_infrastructures[] | {database_engine: (.spec.database.engine // "postgresql"), name: .spec.environmentId})
			]
			| map(select(.database_engine != $database_engine) | "\(.name): \(.database_engine)")
			| join(", ")')

	if [ -n "${changed_database_engines}" ]
	then
		echo "The configuration JSON file requests the database engine \"${database_engine}\", but this deployment already uses another one (${changed_database_engines}). Changing the database engine is not supported. Restore setup.database.engine, or use a new deployment_name." >&2

		exit 1
	fi
}

function get_database_engine {
	local configuration_json_file=${1}

	jq --raw-output '.setup?.database?.engine? // empty' "${configuration_json_file}"
}

function validate_database_engine {
	local configuration_json_file=${1}

	local database_engine

	database_engine=$(get_database_engine "${configuration_json_file}")

	if [ -z "${database_engine}" ]
	then
		echo "The configuration JSON file must contain a key named \"setup.database.engine\" with one of the supported database engines: ${_SUPPORTED_DATABASE_ENGINES[*]}." >&2

		exit 1
	fi

	local supported_database_engine

	for supported_database_engine in "${_SUPPORTED_DATABASE_ENGINES[@]}"
	do
		if [ "${database_engine}" == "${supported_database_engine}" ]
		then
			return
		fi
	done

	echo "The database engine \"${database_engine}\" is not supported. Supported database engines: ${_SUPPORTED_DATABASE_ENGINES[*]}." >&2

	exit 1
}

function _get_custom_resources {
	local custom_resource_definition=${1}

	local output

	output=$( \
		kubectl get \
			--ignore-not-found \
			--output name \
			customresourcedefinition \
			"${custom_resource_definition}") || return 1

	if [ -z "${output}" ]
	then
		echo "[]"

		return
	fi

	kubectl get \
		--all-namespaces \
		--output json \
		"${custom_resource_definition}" | jq '.items'
}