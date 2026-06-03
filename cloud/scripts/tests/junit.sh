#!/usr/bin/env bash

# Minimal JUnit XML emitter for shell-driven tests.
#
# Source this file, call "junit_init <suite-name> <output-file>", record results
# with "junit_case <name> <command> [args...]", then write the report and set the
# exit status with "junit_finish". Each "junit_case" runs the given command,
# captures its combined output, and records a passing or failing testcase based on
# the command's exit code. This lets bash-driven checks (Helm rendering, Crossplane
# render and validate, and similar) produce the same JUnit reports that
# helm-unittest, tflint, checkov, and "terraform test" emit natively, so a single
# GitHub Actions reporter can consume every layer.

JUNIT_FAILURES=0
JUNIT_OUTPUT_FILE=""
JUNIT_SUITE_NAME=""
JUNIT_TESTCASES=""
JUNIT_TESTS=0

function junit_init {
	JUNIT_FAILURES=0
	JUNIT_OUTPUT_FILE="${2}"
	JUNIT_SUITE_NAME="${1}"
	JUNIT_TESTCASES=""
	JUNIT_TESTS=0

	mkdir -p "$(dirname "${JUNIT_OUTPUT_FILE}")"
}

function junit_case {
	local name="${1}"

	shift

	JUNIT_TESTS=$((JUNIT_TESTS + 1))

	local exit_code=0
	local output

	output=$("${@}" 2>&1) || exit_code="${?}"

	local escaped_name
	escaped_name=$(_junit_escape "${name}")

	if [ "${exit_code}" -eq 0 ]
	then
		echo "PASS: ${name}"

		JUNIT_TESTCASES="${JUNIT_TESTCASES}		<testcase classname=\"${JUNIT_SUITE_NAME}\" name=\"${escaped_name}\"/>
"
	else
		echo "FAIL: ${name} (exit code ${exit_code})"
		echo "${output}"

		JUNIT_FAILURES=$((JUNIT_FAILURES + 1))

		local cdata_output="${output//]]>/]]]]><![CDATA[>}"

		JUNIT_TESTCASES="${JUNIT_TESTCASES}		<testcase classname=\"${JUNIT_SUITE_NAME}\" name=\"${escaped_name}\">
			<failure message=\"Command failed with exit code ${exit_code}\"><![CDATA[${cdata_output}]]></failure>
		</testcase>
"
	fi
}

function junit_finish {
	{
		echo "<?xml version=\"1.0\" encoding=\"UTF-8\"?>"
		echo "<testsuites>"
		echo "	<testsuite name=\"${JUNIT_SUITE_NAME}\" tests=\"${JUNIT_TESTS}\" failures=\"${JUNIT_FAILURES}\">"
		printf "%s" "${JUNIT_TESTCASES}"
		echo "	</testsuite>"
		echo "</testsuites>"
	} > "${JUNIT_OUTPUT_FILE}"

	echo ""
	echo "Wrote ${JUNIT_OUTPUT_FILE}: ${JUNIT_TESTS} tests, ${JUNIT_FAILURES} failures."

	if [ "${JUNIT_FAILURES}" -ne 0 ]
	then
		return 1
	fi

	return 0
}

function _junit_escape {
	local text="${1}"

	text="${text//&/&amp;}"
	text="${text//</&lt;}"
	text="${text//>/&gt;}"
	text="${text//\"/&quot;}"

	printf "%s" "${text}"
}
