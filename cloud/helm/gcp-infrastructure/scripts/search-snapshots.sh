#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

function main {
	put_search_api "_snapshot/snapshots" "{\"settings\": {\"bucket\": \"${SNAPSHOT_BUCKET}\"}, \"type\": \"gcs\"}"

	put_search_api "_slm/policy/daily-snapshots" "${SNAPSHOT_POLICY}"
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