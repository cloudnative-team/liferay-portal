#!/usr/bin/env bash

# Runs the Crossplane unit-test suite for a single infrastructure-provider chart
# and writes a JUnit report.
#
# Usage: run-crossplane-tests.sh <chart> <report-directory>
#
# The chart is rendered with Helm to extract its Composition, XRD, and
# EnvironmentConfigs. Every example XR under <chart>/tests/crossplane/xr*.yaml is
# then rendered through the Composition's Function pipeline with "crossplane
# render" and the resulting composite resource is validated against the XRD with
# "crossplane beta validate". "crossplane render" pulls and runs the pipeline
# Functions with Docker.

set -o errexit
set -o nounset
set -o pipefail

function main {
	if [ "${#}" -ne 2 ]
	then
		echo "Usage: ${0} <chart> <report-directory>"

		return 1
	fi

	local chart="${1}"
	local report_directory="${2}"

	local script_directory
	script_directory=$(cd "$(dirname "${0}")" && pwd)

	# shellcheck source=cloud/scripts/tests/junit.sh
	source "${script_directory}/junit.sh"

	local chart_directory
	chart_directory=$(cd "${script_directory}/../../helm/${chart}" && pwd)

	local fixtures_directory="${chart_directory}/tests/crossplane"

	if [ ! -d "${fixtures_directory}" ]
	then
		echo "No Crossplane fixtures were found for chart ${chart}."

		return 0
	fi

	mkdir -p "${report_directory}"

	report_directory=$(cd "${report_directory}" && pwd)

	local work_directory
	work_directory=$(mktemp -d)

	trap 'rm -rf "${work_directory:-}"' EXIT

	helm dependency update --skip-refresh "${chart_directory}" > /dev/null

	local template_args=(liferay "${chart_directory}")

	if [ -f "${fixtures_directory}/values.yaml" ]
	then
		template_args+=(--values "${fixtures_directory}/values.yaml")
	fi

	helm template "${template_args[@]}" > "${work_directory}/all.yaml"

	yq 'select(.kind == "Composition")' \
		"${work_directory}/all.yaml" > "${work_directory}/composition.yaml"
	yq 'select(.kind == "CompositeResourceDefinition")' \
		"${work_directory}/all.yaml" > "${work_directory}/xrd.yaml"
	yq 'select(.kind == "EnvironmentConfig")' \
		"${work_directory}/all.yaml" > "${work_directory}/environment-configs.yaml"

	junit_init "crossplane-${chart}" "${report_directory}/crossplane-${chart}.xml"

	local xr

	for xr in "${fixtures_directory}"/xr*.yaml
	do
		if [ ! -f "${xr}" ]
		then
			continue
		fi

		local name
		name=$(basename "${xr}" .yaml)

		junit_case "render ${name}" \
			_render "${work_directory}" "${fixtures_directory}" "${xr}" "${name}"

		junit_case "validate ${name}" _validate "${work_directory}" "${name}"
	done

	junit_finish
}

function _render {
	local work_directory="${1}"
	local fixtures_directory="${2}"
	local xr="${3}"
	local name="${4}"

	local render_args=(
		"${xr}"
		"${work_directory}/composition.yaml"
		"${fixtures_directory}/functions.yaml"
		--include-full-xr
		--xrd "${work_directory}/xrd.yaml"
	)

	if [ -s "${work_directory}/environment-configs.yaml" ]
	then
		render_args+=(--required-resources "${work_directory}/environment-configs.yaml")
	fi

	crossplane render "${render_args[@]}" > "${work_directory}/rendered-${name}.yaml"
}

function _validate {
	local work_directory="${1}"
	local name="${2}"

	local xr_kind
	xr_kind=$(yq '.spec.names.kind' "${work_directory}/xrd.yaml")

	yq "select(.kind == \"${xr_kind}\")" \
		"${work_directory}/rendered-${name}.yaml" > "${work_directory}/xr-out-${name}.yaml"

	if [ ! -s "${work_directory}/xr-out-${name}.yaml" ]
	then
		echo "No ${xr_kind} composite resource was rendered for ${name}."

		return 1
	fi

	crossplane beta validate \
		"${work_directory}/xrd.yaml" \
		"${work_directory}/xr-out-${name}.yaml" --skip-success-results
}

main "${@}"
