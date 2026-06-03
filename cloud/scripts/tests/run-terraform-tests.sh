#!/usr/bin/env bash

# Runs the unit-test suite for a single Terraform stack and writes JUnit reports.
#
# Usage: run-terraform-tests.sh <stack> <report-directory>
#
# <stack> is a path relative to cloud/terraform, e.g. "aws/ecr". "terraform init"
# and "terraform validate" are recorded in a harness report. tflint, checkov, and
# "terraform test" each emit their own native JUnit report. "terraform test" only
# runs when the stack ships .tftest.hcl files and requires Terraform 1.11 or later
# for JUnit output.

set -o errexit
set -o nounset
set -o pipefail

function main {
	if [ "${#}" -ne 2 ]
	then
		echo "Usage: ${0} <stack> <report-directory>"

		return 1
	fi

	local stack="${1}"
	local report_directory="${2}"

	local script_directory
	script_directory=$(cd "$(dirname "${0}")" && pwd)

	# shellcheck source=cloud/scripts/tests/junit.sh
	source "${script_directory}/junit.sh"

	local stack_directory
	stack_directory=$(cd "${script_directory}/../../terraform/${stack}" && pwd)

	local cloud="${stack%%/*}"
	local stack_id="${stack//\//-}"
	local tflint_config="${script_directory}/../../terraform/${cloud}/.tflint.hcl"

	mkdir -p "${report_directory}"

	report_directory=$(cd "${report_directory}" && pwd)

	cd "${stack_directory}"

	junit_init "terraform-${stack_id}" "${report_directory}/terraform-harness-${stack_id}.xml"

	junit_case "terraform init" terraform init -backend=false -input=false

	junit_case "terraform validate" terraform validate

	local harness_status=0

	junit_finish || harness_status="${?}"

	local tflint_status=0

	tflint --config="${tflint_config}" --init

	tflint --config="${tflint_config}" --format=junit \
		> "${report_directory}/tflint-${stack_id}.xml" || tflint_status="${?}"

	local checkov_status=0

	checkov --compact --directory . --framework terraform --output junitxml \
		> "${report_directory}/checkov-${stack_id}.xml" || checkov_status="${?}"

	local test_status=0

	if compgen -G "*.tftest.hcl" > /dev/null || compgen -G "tests/*.tftest.hcl" > /dev/null
	then
		terraform test -junit-xml="${report_directory}/tftest-${stack_id}.xml" || test_status="${?}"
	else
		echo "No terraform test files were found for stack ${stack}."
	fi

	if [ "${harness_status}" -ne 0 ] || \
		[ "${tflint_status}" -ne 0 ] || \
		[ "${checkov_status}" -ne 0 ] || \
		[ "${test_status}" -ne 0 ]
	then
		return 1
	fi

	return 0
}

main "${@}"
