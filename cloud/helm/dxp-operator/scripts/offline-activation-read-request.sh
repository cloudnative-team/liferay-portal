#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	local identity_secret_name="{{ "{{" }}inputs.parameters.identity-secret-name}}"

	local offline_request

	offline_request=$(
		kubectl \
			get \
			secret \
			"${identity_secret_name}" \
			--output "jsonpath={.data['offline-request']}" | \
			base64 --decode
	)

	if [ -z "${offline_request}" ]
	then
		echo "The operator has not published an offline activation request yet. Confirm that the environment runs in offline mode, then submit this workflow again."

		exit 1
	fi

	printf '\nSend the following offline activation request to Liferay to obtain the offline activation bundle.\n\n%s\n\n' "${offline_request}"
}

main