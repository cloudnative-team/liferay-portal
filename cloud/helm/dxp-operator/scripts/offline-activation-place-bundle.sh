#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	local bundle_name="{{ "{{" }}inputs.parameters.bundle-name}}"
	local bundle_path="{{ "{{" }}inputs.artifacts.offline-activation-bundle.path}}"
	local marketplace_path="{{ "{{" }}inputs.parameters.marketplace-path}}"

	local staged_path="${marketplace_path}/.${bundle_name}"

	cp "${bundle_path}" "${staged_path}"

	mv "${staged_path}" "${marketplace_path}/${bundle_name}"

	echo "The offline activation bundle was placed at ${marketplace_path}/${bundle_name}."
}

main