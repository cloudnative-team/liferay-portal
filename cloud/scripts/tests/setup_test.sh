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