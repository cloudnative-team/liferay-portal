#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	if [ -z "${LIFERAY_ACTIVATION_CODE}" ]
	then
		echo "The activation code is empty." >&2

		exit 1
	fi

	if [ -z "${LIFERAY_SECRET_KEY}" ] || [ -z "${LIFERAY_SECRET_NAME}" ]
	then
		echo "The environment does not reference an activation code secret." >&2

		exit 1
	fi

	kubectl \
		create \
		secret \
		generic \
		"${LIFERAY_SECRET_NAME}" \
		--dry-run=client \
		--from-literal="${LIFERAY_SECRET_KEY}=${LIFERAY_ACTIVATION_CODE}" \
		--output yaml | \
		kubectl \
			apply \
			--field-manager online-activation-workflow \
			--filename - \
			--force-conflicts \
			--server-side

	echo "The activation code was written to the secret ${LIFERAY_SECRET_NAME}."
}

main