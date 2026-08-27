#!/bin/sh

set -o errexit
set -o nounset
set -o pipefail

function main {
	local names

	names=$( \
		kubectl \
			get \
			liferayenvironments \
			--output jsonpath="{.items[*].metadata.name}")

	if [ -z "${names}" ]
	then
		echo "The namespace ${LIFERAY_NAMESPACE} holds no LiferayEnvironment resource to activate." >&2

		exit 1
	fi

	local count

	count=$(echo "${names}" | wc -w | tr -d " ")

	if [ "${count}" -gt 1 ]
	then
		echo "The namespace ${LIFERAY_NAMESPACE} holds ${count} LiferayEnvironment resources, but activation requires exactly one: ${names}." >&2

		exit 1
	fi

	local liferay_environment_name="${names}"

	local phase

	phase=$( \
		kubectl \
			get \
			liferayenvironment \
			"${liferay_environment_name}" \
			--output jsonpath="{.status.phase}")

	if [ "${phase}" != "Pending" ]
	then
		local activated_at

		activated_at=$( \
			kubectl \
				get \
				liferayenvironment \
				"${liferay_environment_name}" \
				--output jsonpath="{.status.activatedAt}")

		if [ -n "${activated_at}" ]
		then
			echo "The environment ${liferay_environment_name} was already activated on ${activated_at}. Re-activation is not supported." >&2

			exit 1
		fi

		if [ -z "${phase}" ]
		then
			echo "The environment ${liferay_environment_name} has no phase yet, which means the operator has not reconciled it. Activation requires the phase \"Pending\"." >&2

			exit 1
		fi

		echo "The environment ${liferay_environment_name} is in the phase \"${phase}\", but activation requires the phase \"Pending\"." >&2

		exit 1
	fi

	printf "%s" "${liferay_environment_name}" > /tmp/liferay-environment-name.txt

	echo "The environment ${liferay_environment_name} is awaiting activation."
}

main
