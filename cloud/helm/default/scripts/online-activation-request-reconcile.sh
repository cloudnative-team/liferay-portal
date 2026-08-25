#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	local liferay_environment_name="{{ "{{" }}workflow.parameters.liferay-environment-name}}"

	local requested_at

	requested_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)

	local patch

	patch=$( \
		jq \
			--arg requestedAt "${requested_at}" \
			--arg requestedBy "${LIFERAY_WORKFLOW_NAME}" \
			--null-input \
			'{
				metadata: {
					annotations: {
						"licensing.liferay.com/activation-requested-at": $requestedAt,
						"licensing.liferay.com/activation-requested-by": $requestedBy
					}
				}
			}')

	kubectl \
		patch \
		liferayenvironment \
		"${liferay_environment_name}" \
		--patch "${patch}" \
		--type merge

	echo "A reconcile of the environment ${liferay_environment_name} was requested."
}

main