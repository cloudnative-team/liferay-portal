#!/usr/bin/env bash

# Runs the full unit-test suite for a single Helm chart and writes JUnit reports.
#
# Usage: run-helm-tests.sh <chart> <report-directory>
#
# The static checks (dependency resolution, lint, the chart's own bash tests, and
# the kubeconform render-validate) are recorded in a harness report. helm-unittest
# suites, when present, are run separately so they keep their native JUnit output.

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

	mkdir -p "${report_directory}"

	report_directory=$(cd "${report_directory}" && pwd)

	junit_init "helm-${chart}" "${report_directory}/helm-harness-${chart}.xml"

	junit_case "helm dependency update" \
		helm dependency update --skip-refresh "${chart_directory}"

	junit_case "helm lint" helm lint "${chart_directory}"

	junit_case "kubeconform render and validate" \
		_render_and_validate "${chart_directory}"

	local test

	for test in \
		"${chart_directory}"/scripts/tests/*-test.sh \
		"${chart_directory}"/tests/*_test.sh
	do
		if [ -f "${test}" ]
		then
			junit_case "bash test $(basename "${test}")" bash "${test}"
		fi
	done

	local harness_status=0

	junit_finish || harness_status="${?}"

	local unittest_status=0

	if compgen -G "${chart_directory}/tests/*_test.yaml" > /dev/null
	then
		helm unittest \
			--output-file "${report_directory}/helm-unittest-${chart}.xml" \
			--output-type JUnit \
			"${chart_directory}" || unittest_status="${?}"
	else
		echo "No helm-unittest suites were found for chart ${chart}."
	fi

	if [ "${harness_status}" -ne 0 ] || [ "${unittest_status}" -ne 0 ]
	then
		return 1
	fi

	return 0
}

function _render_and_validate {
	local chart_directory="${1}"

	helm template liferay "${chart_directory}" | \
		kubeconform --strict --summary \
			-schema-location default \
			-schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
			-skip ClusterProviderConfig,LiferayInfrastructure
}

main "${@}"
