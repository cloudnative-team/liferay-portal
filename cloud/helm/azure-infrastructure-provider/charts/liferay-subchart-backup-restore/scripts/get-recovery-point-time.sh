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

	backup_instance_name=""

	local recovery_point_time

	recovery_point_time=""

	for candidate_backup_instance_name in $( \
		az dataprotection backup-instance list \
			--output tsv \
			--query "[].name" \
			--resource-group "${resource_group_name}" \
			--vault-name "${backup_vault_name}")
	do
		recovery_point_time=$( \
			az dataprotection recovery-point show \
				--backup-instance-name "${candidate_backup_instance_name}" \
				--output tsv \
				--query properties.recoveryPointTime \
				--recovery-point-id "{{ "{{" }}workflow.parameters.recovery-point-id}}" \
				--resource-group "${resource_group_name}" \
				--vault-name "${backup_vault_name}" 2> /dev/null || echo "")

		if [ -n "${recovery_point_time}" ]
		then
			backup_instance_name="${candidate_backup_instance_name}"

			break
		fi
	done

	if [ -z "${backup_instance_name}" ]
	then
		echo "The recovery point {{ "{{" }}workflow.parameters.recovery-point-id}} was not found in the backup vault ${backup_vault_name}." >&2

		exit 1
	fi

	local backup_instance_storage_account_id

	backup_instance_storage_account_id=$( \
		az dataprotection backup-instance show \
			--name "${backup_instance_name}" \
			--output tsv \
			--query properties.dataSourceInfo.resourceID \
			--resource-group "${resource_group_name}" \
			--vault-name "${backup_vault_name}")

	local recovery_point_second

	recovery_point_second=$(echo "${recovery_point_time}" | cut --characters=1-19)

	local document_library_restore_mode

	if [ "${backup_instance_storage_account_id}" == "${storage_account_id}" ]
	then
		document_library_restore_mode="vault"
	else
		document_library_restore_mode="in-place"

		local restore_policy

		restore_policy=$( \
			az storage account blob-service-properties show \
				--account-name "$(basename "${backup_instance_storage_account_id}")" \
				--output tsv \
				--query "{enabled: restorePolicy.enabled, minRestoreTime: restorePolicy.minRestoreTime}" \
				--resource-group "${resource_group_name}")

		local min_restore_second

		min_restore_second=$( \
			echo "${restore_policy}" \
				| cut --fields=2 \
				| cut --characters=1-19)

		if [ "$(echo "${restore_policy}" | cut --fields=1 | tr "[:upper:]" "[:lower:]")" != "true" ] ||
		   [ -z "${min_restore_second}" ] ||
		   [ "$(printf "%s\n%s\n" "${min_restore_second}" "${recovery_point_second}" | sort | head --lines=1)" != "${min_restore_second}" ]
		then
			echo "The recovery point {{ "{{" }}workflow.parameters.recovery-point-id}} was taken from the storage account ${backup_instance_storage_account_id}, which is the restore target, at ${recovery_point_time}, before its point in time restore window opened at ${min_restore_second}. Azure cannot restore a vaulted blob recovery point into the storage account it was taken from, so the document library cannot follow this restore." >&2

			exit 1
		fi

		local container_modified_second

		container_modified_second=$( \
			az storage container-rm show \
				--name document-library \
				--output tsv \
				--query lastModifiedTime \
				--resource-group "${resource_group_name}" \
				--storage-account "$(basename "${backup_instance_storage_account_id}")" 2> /dev/null \
				| cut --characters=1-19 \
				|| echo "")

		if [ -z "${container_modified_second}" ] ||
		   [ "$(printf "%s\n%s\n" "${container_modified_second}" "${recovery_point_second}" | sort | head --lines=1)" != "${container_modified_second}" ]
		then
			echo "The storage account ${backup_instance_storage_account_id} no longer holds the document-library container that was live at ${recovery_point_time}, so the document library cannot be restored in place." >&2

			exit 1
		fi
	fi

	echo "The document library is restored with the ${document_library_restore_mode} mode."

	echo "${backup_instance_name}" > /tmp/backup-instance-name.txt

	echo "${document_library_restore_mode}" > /tmp/document-library-restore-mode.txt

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