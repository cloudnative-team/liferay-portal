#!/bin/sh

set -o errexit
set -o nounset

function main {
	az login \
		--federated-token "$(cat "${AZURE_FEDERATED_TOKEN_FILE}")" \
		--service-principal \
		--tenant "${AZURE_TENANT_ID}" \
		--username "${AZURE_CLIENT_ID}" > /dev/null

	local recovery_point_time

	recovery_point_time="$(echo "{{ "{{" }}inputs.parameters.recovery-point-time}}" | cut --characters=1-19)Z"

	local resource_group_name

	resource_group_name="{{ "{{" }}inputs.parameters.resource-group-name}}"

	local storage_account_name

	storage_account_name="{{ "{{" }}inputs.parameters.storage-account-name}}"

	local restore_status

	restore_status=$( \
		az storage blob restore \
			--account-name "${storage_account_name}" \
			--blob-range document-library document-library0 \
			--output json \
			--query "{failureReason: failureReason, status: status}" \
			--resource-group "${resource_group_name}" \
			--time-to-restore "${recovery_point_time}")

	if [ "$(echo "${restore_status}" | jq --raw-output ".status")" != "Complete" ]
	then
		echo "The point in time restore of ${storage_account_name} to ${recovery_point_time} finished with ${restore_status}." >&2

		exit 1
	fi

	echo "The document library in ${storage_account_name} was restored to ${recovery_point_time}."
}

main