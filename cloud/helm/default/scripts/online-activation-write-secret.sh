#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	local liferay_environment_name="{{ "{{" }}workflow.parameters.liferay-environment-name}}"

	if [ -z "${LIFERAY_ACTIVATION_CODE}" ]
	then
		echo "The activation code is empty." >&2

		exit 1
	fi

	local secret_key

	secret_key=$( \
		kubectl \
			get \
			liferayenvironment \
			"${liferay_environment_name}" \
			--output jsonpath="{.spec.activationCodeSecretRef.key}")

	local secret_name

	secret_name=$( \
		kubectl \
			get \
			liferayenvironment \
			"${liferay_environment_name}" \
			--output jsonpath="{.spec.activationCodeSecretRef.name}")

	if [ -z "${secret_key}" ] || [ -z "${secret_name}" ]
	then
		echo "The environment ${liferay_environment_name} does not reference an activation code secret." >&2

		exit 1
	fi

	kubectl \
		create \
		secret \
		generic \
		"${secret_name}" \
		--dry-run=client \
		--from-literal="${secret_key}=${LIFERAY_ACTIVATION_CODE}" \
		--output yaml | \
		kubectl \
			apply \
			--field-manager online-activation-workflow \
			--filename - \
			--force-conflicts \
			--server-side

	echo "The activation code was written to the secret ${secret_name}."
}

main