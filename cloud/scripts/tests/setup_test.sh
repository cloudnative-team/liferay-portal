#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

function main {
	local fail=0
	local pass=0

	local scripts_dir

	scripts_dir=$(cd "$(dirname "${0}")/.." && pwd)

	for script in "${scripts_dir}/setup_aws.sh" "${scripts_dir}/setup_gcp.sh"
	do
		_run_test "${script}" _test_aborts_with_config_missing_variables_object
		_run_test "${script}" _test_aborts_with_old_terraform_version
	done

	for script in "${scripts_dir}/setup_aws.sh" "${scripts_dir}/setup_azure.sh" "${scripts_dir}/setup_gcp.sh"
	do
		_run_test "${script}" _test_aborts_with_malformed_config_json
		_run_test "${script}" _test_aborts_with_malformed_setup_properties
		_run_test "${script}" _test_aborts_with_missing_config_file
		_run_test "${script}" _test_aborts_with_missing_required_utility
		_run_test "${script}" _test_aborts_with_no_arguments
		_run_test "${script}" _test_aborts_with_unsupported_setup_component
		_run_test "${script}" _test_aborts_with_unsupported_setup_property
		_run_test "${script}" _test_aborts_with_unsupported_setup_property_value
	done

	_run_test "${scripts_dir}/setup_azure.sh" _test_aborts_with_missing_setup_property

	_run_test "${scripts_dir}/_setup_properties.sh" _test_resolves_per_provider_setup_property_defaults
	_run_test "${scripts_dir}/_setup_properties.sh" _test_validates_per_provider_setup_property_values

	_run_test "${scripts_dir}/setup_azure.sh" _test_setup_properties_lock_fails_on_changed_recorded_value
	_run_test "${scripts_dir}/setup_azure.sh" _test_setup_properties_lock_fails_on_changed_unrecorded_value
	_run_test "${scripts_dir}/setup_azure.sh" _test_setup_properties_lock_fails_on_mixed_environments
	_run_test "${scripts_dir}/setup_azure.sh" _test_setup_properties_lock_fails_on_unreachable_cluster
	_run_test "${scripts_dir}/setup_azure.sh" _test_setup_properties_lock_passes_on_empty_cluster
	_run_test "${scripts_dir}/setup_azure.sh" _test_setup_properties_lock_passes_on_unchanged_unrecorded_value
	_run_test "${scripts_dir}/setup_azure.sh" _test_setup_properties_lock_passes_without_cluster

	echo ""
	echo "Results: ${pass} passed, ${fail} failed."

	if [ ${fail} -eq 0 ]
	then
		return 0
	fi

	return 1
}

function _has_rejected_setup_properties {
	local expected_message=${3}
	local script=${1}
	local setup_json=${2}

	local config_json
	local exit_code
	local output
	local result

	config_json=$( \
		jq \
			--argjson setup "${setup_json}" \
			--null-input \
			'{deployment_name: "test", options: {version: "9.9.9"}, region: "eastus2", setup: $setup, subscription_id: "test", tenant_id: "test", variables: {}}')
	result=$(_run_setup_test "${script}" "${config_json}")
	exit_code=$(echo "${result}" | head --lines=1)
	output=$(echo "${result}" | tail --lines=+2)

	if [[ "${exit_code}" -ne 0 ]] &&
	   [[ "${output}" == *"${expected_message}"* ]] &&
	   [[ "${output}" != *"Attempting to log"* ]] &&
	   [[ "${output}" != *"Reusing the active"* ]]
	then
		return 0
	fi

	return 1
}

function _make_az_stub {
	local aks_show_exit_code=${2}
	local stub_dir=${1}

	rm --force "${stub_dir}/az"

	cat > "${stub_dir}/az" <<EOF
#!/usr/bin/env bash

if [ "\${1:-}" == "aks" ] && [ "\${2:-}" == "show" ]
then
	exit "${aks_show_exit_code}"
fi

exit 0
EOF

	chmod +x "${stub_dir}/az"
}

function _make_cloud_dir {
	local registry_json=${2}
	local scripts_dir=${1}

	local cloud_dir

	cloud_dir=$(mktemp --directory)

	mkdir --parents "${cloud_dir}/terraform/azure/aks" "${cloud_dir}/terraform/azure/platform"

	cp --recursive "${scripts_dir}" "${cloud_dir}/scripts"

	printf '%s' "${registry_json}" > "${cloud_dir}/scripts/setup_properties.json"

	echo "${cloud_dir}"
}

function _make_kubectl_stub {
	local resources_json=${2}
	local stub_dir=${1}

	rm --force "${stub_dir}/kubectl"

	if [ -n "${resources_json}" ]
	then
		printf '%s' "${resources_json}" > "${stub_dir}/kubectl-resources.json"
	fi

	cat > "${stub_dir}/kubectl" <<'EOF'
#!/usr/bin/env bash

resource=${*: -1}
resources_file="$(dirname "${0}")/kubectl-resources.json"

if [ ! -f "${resources_file}" ]
then
	echo "Unable to connect to the server: dial tcp 10.0.0.1:443: i/o timeout" >&2

	exit 1
fi

if ! jq \
		--arg resource "${resource}" \
		--exit-status \
		'has($resource)' \
		"${resources_file}" > /dev/null
then
	echo "error: the server doesn't have a resource type \"${resource}\"" >&2

	exit 1
fi

jq --arg resource "${resource}" '{items: .[$resource]}' "${resources_file}"
EOF

	chmod +x "${stub_dir}/kubectl"
}

function _make_stub_path {
	local stub_dir

	stub_dir=$(mktemp -d)

	for util in aws az gcloud helm jq kubectl
	do
		local real_path

		real_path=$(command -v "${util}" 2>/dev/null || true)

		if [[ -n ${real_path} ]]
		then
			ln -s "${real_path}" "${stub_dir}/${util}"
		else
			cat > "${stub_dir}/${util}" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
			chmod +x "${stub_dir}/${util}"
		fi
	done

	for util in awk basename bash dirname env head mktemp sed sort tr
	do
		local real_path

		real_path=$(command -v "${util}" 2>/dev/null || true)

		if [[ -n ${real_path} ]]
		then
			ln -s "${real_path}" "${stub_dir}/${util}"
		fi
	done

	echo "${stub_dir}"
}

function _make_terraform_stub {
	local stub_dir="${1}"
	local version="${2}"

	cat > "${stub_dir}/terraform" <<EOF
#!/usr/bin/env bash

if [[ \${1:-} == --version ]]
then
	echo "Terraform v${version}"

	exit 0
fi

exit 0
EOF

	chmod +x "${stub_dir}/terraform"
}

function _run_setup_properties_lock_test {
	local aks_show_exit_code=${2}
	local resources_json=${3}
	local script=${1}
	local setup_json=${4}

	local scripts_dir

	scripts_dir=$(dirname "${script}")

	local cloud_dir

	cloud_dir=$(_make_cloud_dir "${scripts_dir}" "$(jq '."database.engine".values = ["mysql", "postgresql"]' "${scripts_dir}/setup_properties.json")")

	jq \
		--argjson setup "${setup_json}" \
		--null-input \
		'{deployment_name: "test", region: "eastus2", setup: $setup, subscription_id: "test", tenant_id: "test"}' > "${cloud_dir}/config.json"

	local stub_dir

	stub_dir=$(_make_stub_path)

	_make_az_stub "${stub_dir}" "${aks_show_exit_code}"
	_make_kubectl_stub "${stub_dir}" "${resources_json}"
	_make_terraform_stub "${stub_dir}" "1.10.0"

	local exit_code=0
	local output

	output=$(PATH="${stub_dir}" TMPDIR="${cloud_dir}" bash "${cloud_dir}/scripts/setup_azure.sh" "${cloud_dir}/config.json" 2>&1) || exit_code=${?}

	rm --force --recursive "${cloud_dir}" "${stub_dir}"

	echo "${exit_code}"
	echo "${output}"
}

function _run_setup_test {
	local config_content="${2}"
	local script="${1}"
	local terraform_version="${3:-1.10.0}"
	local utility_to_remove="${4:-}"

	local stub_dir

	stub_dir=$(_make_stub_path)

	_make_terraform_stub "${stub_dir}" "${terraform_version}"

	if [[ -n ${utility_to_remove} ]]
	then
		rm "${stub_dir}/${utility_to_remove}"
	fi

	local tmpdir

	tmpdir=$(mktemp -d)

	printf '%s' "${config_content}" > "${tmpdir}/config.json"

	local exit_code=0
	local output

	output=$(PATH="${stub_dir}" bash "${script}" "${tmpdir}/config.json" 2>&1) || exit_code="${?}"

	rm -rf "${stub_dir}" "${tmpdir}"

	echo "${exit_code}"
	echo "${output}"
}

function _run_test {
	local script="${1}"
	local test_function="${2}"

	local script_name

	script_name=$(basename "${script}")

	local description

	description=$(echo "${test_function}" | sed "s/^_test_//; s/_/ /g")

	local exit_code=0

	"${test_function}" "${script}" || exit_code="${?}"

	if [ ${exit_code} -eq 0 ]
	then
		echo "PASS: [${script_name}] ${description}."

		pass=$((pass + 1))
	else
		echo "FAIL: [${script_name}] ${description}."

		fail=$((fail + 1))
	fi
}

function _test_aborts_with_config_missing_variables_object {
	local config_json
	local exit_code
	local output
	local result

	config_json=$(jq --null-input '{options: {provider: "aws"}}')
	result=$(_run_setup_test "${1}" "${config_json}")
	exit_code=$(echo "${result}" | head -n 1)
	output=$(echo "${result}" | tail -n +2)

	if [ ${exit_code} -ne 0 ] && [[ ${output} == *'must contain a root object named "variables"'* ]]
	then
		return 0
	fi

	return 1
}

function _test_aborts_with_malformed_config_json {
	local exit_code
	local output
	local result

	result=$(_run_setup_test "${1}" '{not valid json')
	exit_code=$(echo "${result}" | head -n 1)
	output=$(echo "${result}" | tail -n +2)

	if [ ${exit_code} -ne 0 ] && [[ ${output} == *"is not valid JSON"* ]]
	then
		return 0
	fi

	return 1
}

function _test_aborts_with_malformed_setup_properties {
	_has_rejected_setup_properties "${1}" '"mysql"' 'The configuration JSON file key "setup" must be an object.' || return 1
	_has_rejected_setup_properties "${1}" '{"database": "mysql"}' 'The setup component "database" must be an object.' || return 1
	_has_rejected_setup_properties "${1}" '{"database": {"engine": ["mysql"]}}' 'The setup property "database.engine" must be a boolean, a number, or a string.'
}

function _test_aborts_with_missing_config_file {
	local exit_code=0
	local output
	local stub_dir

	stub_dir=$(_make_stub_path)

	_make_terraform_stub "${stub_dir}" "1.10.0"

	output=$(PATH="${stub_dir}" bash "${1}" /does/not/exist.json 2>&1) || exit_code="${?}"

	rm -rf "${stub_dir}"

	if [ ${exit_code} -ne 0 ] && [[ ${output} == *"Configuration JSON file"*"does not exist"* ]]
	then
		return 0
	fi

	return 1
}

function _test_aborts_with_missing_required_utility {
	local config_json
	local exit_code
	local output
	local result

	config_json=$(jq --null-input '{variables: {}}')
	result=$(_run_setup_test "${1}" "${config_json}" "1.10.0" jq)
	exit_code=$(echo "${result}" | head -n 1)
	output=$(echo "${result}" | tail -n +2)

	if [ ${exit_code} -ne 0 ] && [[ ${output} == *"utility jq is not installed"* ]]
	then
		return 0
	fi

	return 1
}

function _test_aborts_with_missing_setup_property {
	_has_rejected_setup_properties "${1}" '{}' 'The setup property "database.engine" is required.'
}

function _test_aborts_with_no_arguments {
	local exit_code=0
	local output

	output=$(bash "${1}" 2>&1) || exit_code="${?}"

	if [ ${exit_code} -ne 0 ] && [[ ${output} == *"Usage:"* ]]
	then
		return 0
	fi

	return 1
}

function _test_aborts_with_old_terraform_version {
	local config_json
	local exit_code
	local output
	local result

	config_json=$(jq --null-input '{variables: {}}')
	result=$(_run_setup_test "${1}" "${config_json}" "1.9.9")
	exit_code=$(echo "${result}" | head -n 1)
	output=$(echo "${result}" | tail -n +2)

	if [ ${exit_code} -ne 0 ] && [[ ${output} == *"is older than"* ]]
	then
		return 0
	fi

	return 1
}

function _test_aborts_with_unsupported_setup_component {
	_has_rejected_setup_properties "${1}" '{"search": {"engine": "opensearch"}}' 'The setup component "search" is not supported by version 9.9.9.'
}

function _test_aborts_with_unsupported_setup_property {
	_has_rejected_setup_properties "${1}" '{"database": {"engine": "postgresql", "version": "16"}}' 'The setup property "database.version" is not supported by version 9.9.9.'
}

function _test_aborts_with_unsupported_setup_property_value {
	_has_rejected_setup_properties "${1}" '{"database": {"engine": "mysql"}}' 'The value "mysql" of the setup property "database.engine" is not supported on'
}

function _test_resolves_per_provider_setup_property_defaults {
	local scripts_dir

	scripts_dir=$(dirname "${1}")

	local cloud_dir

	cloud_dir=$(_make_cloud_dir "${scripts_dir}" "$(jq '."database.engine".default = {aws: "postgresql", azure: "mysql"}' "${scripts_dir}/setup_properties.json")")

	local engines=""

	local setup_json

	for setup_json in '{}' '{"database": {"engine": "postgresql"}}'
	do
		jq \
			--argjson setup "${setup_json}" \
			--null-input \
			'{setup: $setup}' > "${cloud_dir}/config.json"

		local provider

		for provider in aws azure gcp
		do
			engines+=$(bash -c 'source "${1}" && get_resolved_setup_properties "${2}" "${3}"' _ "${cloud_dir}/scripts/_setup_properties.sh" "${cloud_dir}/config.json" "${provider}" | jq --raw-output '.database.engine // "none"')
			engines+=" "
		done
	done

	rm --force --recursive "${cloud_dir}"

	if [ "${engines}" == "postgresql mysql none postgresql postgresql postgresql " ]
	then
		return 0
	fi

	return 1
}

function _test_setup_properties_lock_fails_on_changed_recorded_value {
	local exit_code
	local output
	local resources_json
	local result

	resources_json='{"environmentconfigs.apiextensions.crossplane.io": [{"data": {"setup": {"database": {"engine": "mysql"}}}, "metadata": {"name": "environment-config"}}], "liferayinfrastructures.liferay.com": [{"metadata": {"name": "liferay-prd"}, "spec": {"database": {"engine": "mysql"}, "environmentId": "prd"}}]}'
	result=$(_run_setup_properties_lock_test "${1}" 0 "${resources_json}" '{"database": {"engine": "postgresql"}}')
	exit_code=$(echo "${result}" | head --lines=1)
	output=$(echo "${result}" | tail --lines=+2)

	if [[ "${exit_code}" -ne 0 ]] &&
	   [[ "${output}" == *'The setup property "database.engine" is "mysql" on this deployment (environments: prd), but the configuration JSON file requests "postgresql".'* ]]
	then
		return 0
	fi

	return 1
}

function _test_setup_properties_lock_fails_on_changed_unrecorded_value {
	local exit_code
	local output
	local resources_json
	local result

	resources_json='{"environmentconfigs.apiextensions.crossplane.io": [{"data": {}, "metadata": {"name": "environment-config"}}], "liferayinfrastructures.liferay.com": [{"metadata": {"name": "liferay-prd"}, "spec": {"environmentId": "prd"}}]}'
	result=$(_run_setup_properties_lock_test "${1}" 0 "${resources_json}" '{"database": {"engine": "mysql"}}')
	exit_code=$(echo "${result}" | head --lines=1)
	output=$(echo "${result}" | tail --lines=+2)

	if [[ "${exit_code}" -ne 0 ]] &&
	   [[ "${output}" == *'The setup property "database.engine" is "postgresql" on this deployment (environments: prd), but the configuration JSON file requests "mysql".'* ]]
	then
		return 0
	fi

	return 1
}

function _test_setup_properties_lock_fails_on_mixed_environments {
	local exit_code
	local output
	local resources_json
	local result

	resources_json='{"environmentconfigs.apiextensions.crossplane.io": [{"data": {"setup": {"database": {"engine": "postgresql"}}}, "metadata": {"name": "environment-config"}}], "liferayinfrastructures.gcp.liferay.com": [{"metadata": {"name": "liferay-uat"}, "spec": {"database": {"engine": "mysql"}, "environmentId": "uat"}}], "liferayinfrastructures.liferay.com": [{"metadata": {"name": "liferay-prd"}, "spec": {"database": {"engine": "postgresql"}, "environmentId": "prd"}}]}'
	result=$(_run_setup_properties_lock_test "${1}" 0 "${resources_json}" '{"database": {"engine": "postgresql"}}')
	exit_code=$(echo "${result}" | head --lines=1)
	output=$(echo "${result}" | tail --lines=+2)

	if [[ "${exit_code}" -ne 0 ]] &&
	   [[ "${output}" == *'but these environments use a different value: uat (mysql).'* ]]
	then
		return 0
	fi

	return 1
}

function _test_setup_properties_lock_fails_on_unreachable_cluster {
	local exit_code
	local output
	local result

	result=$(_run_setup_properties_lock_test "${1}" 0 "" '{"database": {"engine": "mysql"}}')
	exit_code=$(echo "${result}" | head --lines=1)
	output=$(echo "${result}" | tail --lines=+2)

	if [[ "${exit_code}" -ne 0 ]] &&
	   [[ "${output}" == *"Unable to read environmentconfigs.apiextensions.crossplane.io from the Kubernetes cluster."* ]] &&
	   [[ "${output}" != *"The setup properties match"* ]]
	then
		return 0
	fi

	return 1
}

function _test_setup_properties_lock_passes_on_empty_cluster {
	local output
	local result

	result=$(_run_setup_properties_lock_test "${1}" 0 '{}' '{"database": {"engine": "mysql"}}')
	output=$(echo "${result}" | tail --lines=+2)

	if [[ "${output}" == *"The setup properties match the existing deployment."* ]]
	then
		return 0
	fi

	return 1
}

function _test_setup_properties_lock_passes_on_unchanged_unrecorded_value {
	local output
	local resources_json
	local result

	resources_json='{"environmentconfigs.apiextensions.crossplane.io": [{"data": {}, "metadata": {"name": "environment-config"}}], "liferayinfrastructures.liferay.com": [{"metadata": {"name": "liferay-prd"}, "spec": {"environmentId": "prd"}}]}'
	result=$(_run_setup_properties_lock_test "${1}" 0 "${resources_json}" '{"database": {"engine": "postgresql"}}')
	output=$(echo "${result}" | tail --lines=+2)

	if [[ "${output}" == *"The setup properties match the existing deployment."* ]]
	then
		return 0
	fi

	return 1
}

function _test_setup_properties_lock_passes_without_cluster {
	local output
	local result

	result=$(_run_setup_properties_lock_test "${1}" 3 "" '{"database": {"engine": "mysql"}}')
	output=$(echo "${result}" | tail --lines=+2)

	if [[ "${output}" == *"The AKS cluster test-aks does not exist. Skipping the setup properties check."* ]]
	then
		return 0
	fi

	return 1
}

function _test_validates_per_provider_setup_property_values {
	local scripts_dir

	scripts_dir=$(dirname "${1}")

	local cloud_dir

	cloud_dir=$(_make_cloud_dir "${scripts_dir}" "$(jq '."database.engine".values = {aws: ["postgresql"], azure: ["mysql", "postgresql"]}' "${scripts_dir}/setup_properties.json")")

	jq --null-input '{setup: {database: {engine: "mysql"}}}' > "${cloud_dir}/config.json"

	local outputs=()

	local provider

	for provider in aws azure gcp
	do
		outputs+=("$(bash -c 'source "${1}" && validate_setup_properties "${2}" "${3}" && echo "valid"' _ "${cloud_dir}/scripts/_setup_properties.sh" "${cloud_dir}/config.json" "${provider}" 2>&1)")
	done

	rm --force --recursive "${cloud_dir}"

	if [ "${outputs[0]}" == 'The value "mysql" of the setup property "database.engine" is not supported on aws. Supported values: postgresql.' ] &&
	   [ "${outputs[1]}" == "valid" ] &&
	   [ "${outputs[2]}" == 'The setup property "database.engine" is not supported on gcp.' ]
	then
		return 0
	fi

	return 1
}

main "${@}"