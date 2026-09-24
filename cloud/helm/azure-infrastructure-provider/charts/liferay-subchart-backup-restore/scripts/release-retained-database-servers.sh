#!/bin/sh

set -o errexit
set -o nounset

function main {
	local liferay_infrastructure_json

	liferay_infrastructure_json=$( \
		kubectl get liferayinfrastructure \
			--output json \
			| jq ".items[0]")

	if [ "${liferay_infrastructure_json}" == "null" ]
	then
		echo "No LiferayInfrastructure was found in the workflow namespace, so there is nothing to release."

		exit 0
	fi

	local restore_phase

	restore_phase=$(echo "${liferay_infrastructure_json}" | jq --raw-output ".spec.restorePhase // \"none\"")

	if [ "${restore_phase}" != "none" ]
	then
		echo "The LiferayInfrastructure spec.restorePhase is set to ${restore_phase}. A restore is in progress, so no retained resource was released."

		exit 0
	fi

	local expired_backup_instances

	expired_backup_instances=$( \
		echo "${liferay_infrastructure_json}" \
			| jq --compact-output "(.spec.retainedBackupInstances // {}) | with_entries(select(.value | fromdateiso8601 < now)) | with_entries(.value = null)")

	local expired_database_servers

	expired_database_servers=$( \
		echo "${liferay_infrastructure_json}" \
			| jq --compact-output "(.spec.retainedDatabaseServers // {}) | with_entries(select(.value | fromdateiso8601 < now)) | with_entries(.value = null)")

	if [ "${expired_backup_instances}" == "{}" ] && [ "${expired_database_servers}" == "{}" ]
	then
		echo "No retained backup instance or database server has passed its deadline."

		exit 0
	fi

	kubectl patch liferayinfrastructure \
		"$(echo "${liferay_infrastructure_json}" | jq --raw-output ".metadata.name")" \
		--field-manager=liferay-backup-restore \
		--patch "{\"spec\":{\"retainedBackupInstances\":${expired_backup_instances},\"retainedDatabaseServers\":${expired_database_servers}}}" \
		--type merge

	echo "The retained backup instances $(echo "${expired_backup_instances}" | jq --raw-output "keys | join(\", \")") and database servers $(echo "${expired_database_servers}" | jq --raw-output "keys | join(\", \")") were released."
}

main