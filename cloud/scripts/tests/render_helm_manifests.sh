#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

function main {
	local requested_chart="${1:-}"

	local script_dir

	script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

	local cloud_dir

	cloud_dir=$(cd "${script_dir}/../.." && pwd)

	local charts=(
		aws
		aws-infrastructure-provider
		aws-marketplace
		azure
		azure-infrastructure-provider
		client-extension
		default
		dxp-operator
		gcp
		gcp-infrastructure
		gcp-infrastructure-provider
		infrastructure
		observability
		observability-paas
		platform
		platform-components
	)

	if [[ -n ${requested_chart} ]]
	then
		if [[ ! -d ${cloud_dir}/helm/${requested_chart} ]]
		then
			echo "Unable to find chart ${requested_chart}"

			exit 1
		fi

		charts=("${requested_chart}")
	fi

	for chart in "${charts[@]}"
	do
		_update_dependencies "${cloud_dir}/helm/${chart}"

		local helm_template_args=()

		if [[ -f ${script_dir}/render-values/${chart}.yaml ]]
		then
			helm_template_args=("--values" "${script_dir}/render-values/${chart}.yaml")
		fi

		helm template liferay "${cloud_dir}/helm/${chart}" "${helm_template_args[@]}" | kubeconform \
			--schema-location default \
			--schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
			--skip ClientExtension,ClusterProviderConfig,LiferayEnvironment,LiferayInfrastructure,LiferayOverlay,PrivateDNSZone,PrivateDNSZoneVirtualNetworkLink,Subnet \
			--strict \
			--summary
	done
}

function _update_dependencies {
	local chart_dir="${1}"

	local repository

	for repository in $(yq '.dependencies[]? | select(.repository | test("^file://")) | .repository' "${chart_dir}/Chart.yaml")
	do
		_update_dependencies "${chart_dir}/${repository#file://}"
	done

	helm dependency update "${chart_dir}"
}

main "${@}"