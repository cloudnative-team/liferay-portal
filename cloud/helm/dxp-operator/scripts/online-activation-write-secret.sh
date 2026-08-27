#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	local secret_key="{{ "{{" }}inputs.parameters.secret-key}}"
	local secret_name="{{ "{{" }}inputs.parameters.secret-name}}"

	if [ -z "${LIFERAY_ACTIVATION_CODE}" ]
	then
		echo "The activation code is empty." >&2

		exit 1
	fi

	if [ -z "${secret_key}" ] || [ -z "${secret_name}" ]
	then
		echo "The environment does not reference an activation code secret." >&2

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
