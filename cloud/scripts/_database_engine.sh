#!/usr/bin/env bash

_SUPPORTED_DATABASE_ENGINES=(
	"postgresql"
)

readonly _SUPPORTED_DATABASE_ENGINES

function get_database_engine {
	local configuration_json_file=${1}

	jq --raw-output '.setup?.database?.engine? // empty' "${configuration_json_file}"
}

function validate_database_engine {
	local configuration_json_file=${1}

	local database_engine

	database_engine=$(get_database_engine "${configuration_json_file}")

	if [ -z "${database_engine}" ]
	then
		echo "The configuration JSON file must contain a key named \"setup.database.engine\" with one of the supported database engines: ${_SUPPORTED_DATABASE_ENGINES[*]}." >&2

		exit 1
	fi

	local supported_database_engine

	for supported_database_engine in "${_SUPPORTED_DATABASE_ENGINES[@]}"
	do
		if [ "${database_engine}" == "${supported_database_engine}" ]
		then
			return
		fi
	done

	echo "The database engine \"${database_engine}\" is not supported. Supported database engines: ${_SUPPORTED_DATABASE_ENGINES[*]}." >&2

	exit 1
}