#!/usr/bin/env bash

_SETUP_PROPERTIES_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/setup_properties.json"

readonly _SETUP_PROPERTIES_FILE

function assert_setup_properties_unchanged {
	local configuration_json_file=${1}
	local provider=${2}

	local environment_configs

	environment_configs=$(_get_kubernetes_items environmentconfigs.apiextensions.crossplane.io)

	local group
	local liferay_infrastructures="[]"

	for group in aws.liferay.com gcp.liferay.com liferay.com
	do
		local items

		items=$(_get_kubernetes_items "liferayinfrastructures.${group}")

		liferay_infrastructures=$(jq --argjson items "${items}" '. + $items' <<< "${liferay_infrastructures}")
	done

	local requested_setup_properties

	requested_setup_properties=$(get_resolved_setup_properties "${configuration_json_file}" "${provider}")

	local errors

	errors=$( \
		jq \
			--argjson environment_configs "${environment_configs}" \
			--argjson liferay_infrastructures "${liferay_infrastructures}" \
			--argjson requested_setup_properties "${requested_setup_properties}" \
			--null-input \
			--raw-output \
			--slurpfile registry "${_SETUP_PROPERTIES_FILE}" \
			'($environment_configs | map(select(.metadata.name == "environment-config")) | first) as $environment_config
			| $registry[0]
			| to_entries[]
			| select(.value.immutable)
			| .key as $name
			| .value as $property
			| ($name | split(".")) as $path
			| ($requested_setup_properties | getpath($path)) as $requested_value
			| ($environment_config.data.setup | getpath($path)) as $recorded_value
			| select(($recorded_value != null) or ($environment_config != null) or ($liferay_infrastructures | length > 0))
			| ($recorded_value // $property.unrecordedValue) as $recorded_value
			| select($recorded_value != null)
			| $liferay_infrastructures
			| map(
				{
					environment: (.spec.environmentId // .metadata.name),
					value: (if $property.xrPath then getpath($property.xrPath | split(".")) // $property.unrecordedValue else $recorded_value end)
				})
			| (map(.environment) | unique | join(", ")) as $environments
			| (map(select(.value != $recorded_value) | "\(.environment) (\(.value))") | join(", ")) as $mismatched_environments
			| if $mismatched_environments != "" then
				"The setup property \"\($name)\" is \"\($recorded_value)\" on this deployment, but these environments use a different value: \($mismatched_environments). Deployments with mixed values are not supported."
			elif $requested_value != $recorded_value then
				"The setup property \"\($name)\" is \"\($recorded_value)\" on this deployment\(if $environments == "" then "" else " (environments: \($environments))" end), but the configuration JSON file requests \"\($requested_value)\". Changing it and migrating to a different value are not supported. Restore setup.\($name) to \"\($recorded_value)\", or use a new deployment_name."
			else
				empty
			end')

	if [ -n "${errors}" ]
	then
		echo "${errors}" >&2

		exit 1
	fi
}

function get_resolved_setup_properties {
	local configuration_json_file=${1}
	local provider=${2}

	jq \
		--arg provider "${provider}" \
		--slurpfile registry "${_SETUP_PROPERTIES_FILE}" \
		'(reduce ($registry[0] | to_entries[]) as $entry (
			{};
			($entry.value.default | if type == "object" then .[$provider] else . end) as $default
			| if $default == null then . else setpath($entry.key | split("."); $default) end))
		* (.setup // {})' \
		"${configuration_json_file}"
}

function get_setup_properties_platform_values {
	local configuration_json_file=${1}
	local provider=${2}

	get_resolved_setup_properties "${configuration_json_file}" "${provider}" | jq '{setup: .}'
}

function validate_setup_properties {
	local configuration_json_file=${1}
	local provider=${2}

	local errors

	errors=$( \
		jq \
			--arg provider "${provider}" \
			--raw-output \
			--slurpfile registry "${_SETUP_PROPERTIES_FILE}" \
			'(.options.version // "" | if . == "" or . == "latest" then "this version" else "version \(.)" end) as $version
			| $registry[0] as $registry
			| (.setup // {}) as $setup
			| if has("setup") | not then
				empty
			elif .setup | type != "object" then
				"The configuration JSON file key \"setup\" must be an object."
			else
				.setup
				| to_entries[]
				| .key as $component
				| if .value | type != "object" then
					"The setup component \"\($component)\" must be an object."
				elif [$registry | keys[] | split(".")[0]] | any(. == $component) | not then
					"The setup component \"\($component)\" is not supported by \($version)."
				else
					.value
					| to_entries[]
					| "\($component).\(.key)" as $name
					| $registry[$name] as $property
					| .value as $value
					| ($property.values | if type == "object" then .[$provider] // [] else . end) as $values
					| if $property == null then
						"The setup property \"\($name)\" is not supported by \($version)."
					elif $value | type | IN("boolean", "number", "string") | not then
						"The setup property \"\($name)\" must be a boolean, a number, or a string."
					elif $values == [] then
						"The setup property \"\($name)\" is not supported on \($provider)."
					elif $values | any(. == $value) | not then
						"The value \"\($value)\" of the setup property \"\($name)\" is not supported on \($provider). Supported values: \($values | join(", "))."
					else
						empty
					end
				end
			end,
			(
				$registry
				| to_entries[]
				| .key as $name
				| select((.value.default | if type == "object" then .[$provider] else . end) == null)
				| select($setup | type == "object")
				| select((try ($setup | getpath($name | split("."))) catch "malformed") == null)
				| "The setup property \"\($name)\" is required."
			)' \
			"${configuration_json_file}")

	if [ -n "${errors}" ]
	then
		echo "${errors}" >&2

		exit 1
	fi
}

function _get_kubernetes_items {
	local resource=${1}

	local output

	if output=$( \
		kubectl get \
			--all-namespaces \
			--output json \
			"${resource}" 2> /dev/null)
	then
		jq '.items' <<< "${output}"

		return
	fi

	local error

	error=$( \
		kubectl get \
			--all-namespaces \
			--output json \
			"${resource}" 2>&1 > /dev/null) || true

	if [[ "${error}" == *"the server doesn't have a resource type"* ]]
	then
		echo "[]"

		return
	fi

	echo "Unable to read ${resource} from the Kubernetes cluster. ${error}" >&2

	return 1
}