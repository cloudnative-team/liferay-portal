#!/bin/sh

set -o errexit
set -o nounset

function main {
	local liferay_infrastructure_json

	liferay_infrastructure_json=$( \
		kubectl get liferayinfrastructure \
			--output json \
			| jq ".items[0]")

	local restore_phase

	restore_phase=$(echo "${liferay_infrastructure_json}" | jq --raw-output ".spec.restorePhase")

	if [ "${restore_phase}" == "promoting" ] || [ "${restore_phase}" == "provisioning" ]
	then
		echo "The LiferayInfrastructure spec.restorePhase is set to ${restore_phase}. A restore is in progress." >&2

		exit 1
	fi

	echo "${liferay_infrastructure_json}" | jq --raw-output ".metadata.name" > /tmp/liferay-infrastructure-name.txt

	local data_plane_active

	data_plane_active=$(echo "${liferay_infrastructure_json}" | jq --raw-output ".spec.targetActiveDataPlane // \"blue\"")

	echo "${data_plane_active}" > /tmp/data-plane-active.txt

	local data_plane_inactive

	if [ "${data_plane_active}" == "blue" ]
	then
		data_plane_inactive="green"
	else
		data_plane_inactive="blue"
	fi

	echo "${data_plane_inactive}" > /tmp/data-plane-inactive.txt

	if [ -n "$(kubectl get flexibleservers.dbforpostgresql.azure.m.upbound.io --output jsonpath="{.items[*].metadata.name}" --selector "dataPlane=${data_plane_inactive}")" ]
	then
		echo "The ${data_plane_inactive} data plane still holds a database server that is being released. Retry the restore once it is gone." >&2

		exit 1
	fi

	if [ -n "$(kubectl get accounts.storage.azure.m.upbound.io --output jsonpath="{.items[*].metadata.name}" --selector "dataPlane=${data_plane_inactive}")" ]
	then
		echo "The ${data_plane_inactive} data plane still holds a storage account that is being released. Retry the restore once it is gone." >&2

		exit 1
	fi

	kubectl get backupvaults.dataprotection.azure.m.upbound.io \
		--output jsonpath="{.items[0].metadata.name}" \
		> /tmp/backup-vault-name.txt

	kubectl get flexibleservers.dbforpostgresql.azure.m.upbound.io \
		--output jsonpath="{.items[0].metadata.name}" \
		--selector "dataPlane=${data_plane_active}" \
		> /tmp/database-server-name-active.txt

	local database_server_names_retained

	database_server_names_retained=$( \
		kubectl get flexibleservers.dbforpostgresql.azure.m.upbound.io \
			--output jsonpath="{.items[*].metadata.annotations.crossplane\.io/external-name}" \
			--selector "retainedDatabaseServer=true")

	echo "$(cat /tmp/database-server-name-active.txt) ${database_server_names_retained}" \
		| jq --compact-output --raw-input 'split(" ") | map(select(. != ""))' \
		> /tmp/database-server-names.txt

	local retained_until

	retained_until=$( \
		echo "${liferay_infrastructure_json}" \
			| jq --raw-output '([7, ([35, (.spec.backup.retentionDays // 30)] | min)] | max) as $retention_days | ([$retention_days, (.spec.backup.retainedDatabaseServerDays // 7)] | min) as $retained_days | (now + ($retained_days * 86400)) | floor | todate')

	jq \
		--arg name "$(cat /tmp/database-server-name-active.txt)" \
		--arg retained_until "${retained_until}" \
		--compact-output \
		--null-input \
		'{($name): $retained_until}' \
		> /tmp/retained-database-server.txt

	jq \
		--arg name "$(cat /tmp/database-server-name-active.txt)" \
		--compact-output \
		--null-input \
		'{($name): null}' \
		> /tmp/retained-database-server-release.txt

	kubectl get backupinstanceblobstorages.dataprotection.azure.m.upbound.io \
		--output jsonpath="{.items[0].metadata.name}" \
		--selector "dataPlane=${data_plane_active}" \
		> /tmp/backup-instance-name-active.txt 2> /dev/null || true

	jq \
		--arg name "$(cat /tmp/backup-instance-name-active.txt)" \
		--arg retained_until "${retained_until}" \
		--compact-output \
		--null-input \
		'if $name == "" then {} else {($name): $retained_until} end' \
		> /tmp/retained-backup-instance.txt

	jq \
		--arg name "$(cat /tmp/backup-instance-name-active.txt)" \
		--compact-output \
		--null-input \
		'if $name == "" then {} else {($name): null} end' \
		> /tmp/retained-backup-instance-release.txt

	kubectl get flexibleservers.dbforpostgresql.azure.m.upbound.io \
		--output jsonpath="{.items[0].spec.forProvider.resourceGroupName}" \
		--selector "dataPlane=${data_plane_active}" \
		> /tmp/resource-group-name.txt

	kubectl get statefulset \
		--output jsonpath="{.items[0].metadata.name}" \
		--selector "component=liferay" \
		> /tmp/liferay-workload-name.txt

	kubectl get accounts.storage.azure.m.upbound.io \
		--output jsonpath="{.items[0].status.atProvider.id}" \
		--selector "dataPlane=${data_plane_active}" \
		> /tmp/storage-account-id-active.txt
}

main