#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	local liferay_environment_name="{{ "{{" }}inputs.parameters.liferay-environment-name}}"

	local timeout

	timeout=$(($(date +%s) + {{ .Values.licensing.onlineActivationWorkflow.waitTimeoutSeconds }}))

	while [ "$(date +%s)" -lt "${timeout}" ]
	do
		local activated_condition

		activated_condition=$( \
			kubectl \
				get \
				liferayenvironment \
				"${liferay_environment_name}" \
				--output jsonpath="{.status.conditions[?(@.type=='Activated')]}" 2>/dev/null || echo "{}")

		if [ -z "${activated_condition}" ]
		then
			activated_condition="{}"
		fi

		local reason

		reason=$(echo "${activated_condition}" | jq --raw-output ".reason // \"Unknown\"")

		local status

		status=$(echo "${activated_condition}" | jq --raw-output ".status // \"Unknown\"")

		if [ "${status}" = "True" ]
		then
			echo "The environment ${liferay_environment_name} is activated."

			exit 0
		fi

		if [ "${reason}" = "ActivationRejected" ]
		then
			echo "The activation was rejected. $(echo "${activated_condition}" | jq --raw-output ".message // \"\"")"

			exit 1
		fi

		echo "The system is waiting for activation. The current reason is ${reason}."

		sleep 10
	done

	echo "The system timed out waiting for the environment ${liferay_environment_name} to activate."

	exit 1
}

main