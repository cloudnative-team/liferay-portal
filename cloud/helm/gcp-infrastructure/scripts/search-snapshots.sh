#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

function delete_search_api {
	echo "Deleting ${1}."

	local status_code

	status_code=$(
		curl \
			--output /dev/stderr \
			--request DELETE \
			--show-error \
			--silent \
			--user "${SEARCH_USERNAME}:${SEARCH_PASSWORD}" \
			--write-out "%{http_code}" \
			"${SEARCH_URL}/${1}")

	echo ""

	if [ "${status_code}" != "200" ] && [ "${status_code}" != "404" ]
	then
		echo "Unable to delete ${1}. The status code was ${status_code}."

		return 1
	fi
}

function main {
	if [ "${SNAPSHOT_ENABLED}" == "true" ]
	then
		put_search_api "_snapshot/snapshots" "{\"settings\": {\"bucket\": \"${SNAPSHOT_BUCKET}\"}, \"type\": \"gcs\"}"

		put_search_api "_slm/policy/daily-snapshots" "${SNAPSHOT_POLICY}"
	else
		delete_search_api "_slm/policy/daily-snapshots"

		delete_search_api "_snapshot/snapshots"
	fi
}

function put_search_api {
	echo "Updating ${1}."

	curl \
		--data "${2}" \
		--fail-with-body \
		--header "Content-Type: application/json" \
		--request PUT \
		--show-error \
		--silent \
		--user "${SEARCH_USERNAME}:${SEARCH_PASSWORD}" \
		"${SEARCH_URL}/${1}"

	echo ""
}

main