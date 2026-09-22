#!/bin/sh

set -o errexit
set -o nounset

function main {
	az extension add \
		--name dataprotection \
		--version {{ .Values.images.azureCli.dataprotectionExtensionVersion }} \
		--yes > /dev/null

	az login \
		--federated-token "$(cat "${AZURE_FEDERATED_TOKEN_FILE}")" \
		--service-principal \
		--tenant "${AZURE_TENANT_ID}" \
		--username "${AZURE_CLIENT_ID}" > /dev/null

	local backup_vault_name

	backup_vault_name="{{ "{{" }}inputs.parameters.backup-vault-name}}"

	local resource_group_name

	resource_group_name="{{ "{{" }}inputs.parameters.resource-group-name}}"

	local storage_account_id

	storage_account_id="{{ "{{" }}inputs.parameters.storage-account-id}}"

	local backup_instance_name

	backup_instance_name=$( \
		az dataprotection backup-instance list \
			--output tsv \
			--query "[?properties.dataSourceInfo.resourceID=='${storage_account_id}'].name | [0]" \
			--resource-group "${resource_group_name}" \
			--vault-name "${backup_vault_name}")

	if [ -z "${backup_instance_name}" ]
	then
		echo "No backup instance protects the storage account ${storage_account_id}." >&2

		exit 1
	fi

	echo "${backup_instance_name}" > /tmp/backup-instance-name.txt

	local recovery_point_time

	recovery_point_time=$( \
		az dataprotection recovery-point show \
			--backup-instance-name "${backup_instance_name}" \
			--output tsv \
			--query properties.recoveryPointTime \
			--recovery-point-id "{{ "{{" }}workflow.parameters.recovery-point-id}}" \
			--resource-group "${resource_group_name}" \
			--vault-name "${backup_vault_name}")

	if [ -z "${recovery_point_time}" ]
	then
		echo "The recovery point {{ "{{" }}workflow.parameters.recovery-point-id}} has no recovery point time." >&2

		exit 1
	fi

	local recovery_point_second

	recovery_point_second=$(echo "${recovery_point_time}" | cut --characters=1-19)

	local restore_source_earliest_restore_second

	restore_source_earliest_restore_second=""

	local restore_source_server_id

	restore_source_server_id=""

	for database_server_name in $(echo '{{ "{{" }}inputs.parameters.database-server-names}}' | jq --raw-output ".[]")
	do
		local database_server_details

		database_server_details=$( \
			az postgres flexible-server show \
				--name "${database_server_name}" \
				--output tsv \
				--query "{earliestRestoreDate: backup.earliestRestoreDate, id: id}" \
				--resource-group "${resource_group_name}")

		local earliest_restore_date

		earliest_restore_date=$(echo "${database_server_details}" | cut --fields=1)

		if [ -z "${earliest_restore_date}" ]
		then
			echo "The server ${database_server_name} reports no earliest restore date, so its point in time window has not opened yet."

			continue
		fi

		local earliest_restore_second

		earliest_restore_second=$(echo "${earliest_restore_date}" | cut --characters=1-19)

		if [ "$(printf "%s\n%s\n" "${earliest_restore_second}" "${recovery_point_second}" | sort | head --lines=1)" != "${earliest_restore_second}" ]
		then
			echo "The server ${database_server_name} opened its point in time window at ${earliest_restore_date}, after the recovery point time ${recovery_point_time}."

			continue
		fi

		if [ "$(printf "%s\n%s\n" "${earliest_restore_second}" "${restore_source_earliest_restore_second}" | sort | tail --lines=1)" == "${earliest_restore_second}" ]
		then
			restore_source_earliest_restore_second="${earliest_restore_second}"
			restore_source_server_id=$(echo "${database_server_details}" | cut --fields=2)
		fi
	done

	if [ -z "${restore_source_server_id}" ]
	then
		echo "No database server holds the recovery point time ${recovery_point_time} in its point in time window, so the database cannot be paired with it." >&2

		exit 1
	fi

	echo "The database is restored from ${restore_source_server_id}."

	echo "${recovery_point_time}" > /tmp/recovery-point-time.txt

	echo "${restore_source_server_id}" > /tmp/restore-source-server-id.txt
}

main