#!/usr/bin/env bash

_SETUP_PROPERTIES_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/setup_properties.json"

readonly _SETUP_PROPERTIES_FILE

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