#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	local liferay_environment_name="{{ "{{" }}workflow.parameters.liferay-environment-name}}"

	local activated_at

	activated_at=$( \
		kubectl \
			get \
			liferayenvironment \
			"${liferay_environment_name}" \
			--output jsonpath="{.status.activatedAt}")

	if [ -n "${activated_at}" ]
	then
		echo "The environment ${liferay_environment_name} was already activated on ${activated_at}. Re-activation is not supported." >&2

		exit 1
	fi

	echo "The environment ${liferay_environment_name} is awaiting activation."
}

main