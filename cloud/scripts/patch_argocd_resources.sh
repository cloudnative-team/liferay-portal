#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

_ARGOCD_NAMESPACE="argocd-system"

_SHORT_SHA=$(git rev-parse --short=9 HEAD)

function _die {
	echo "ERROR: ${*}" >&2
	exit 1
}

function _log {
	echo ">>> ${*}"
}

function _parse_pr_url {
	local pr_url="${1}"

	if [[ ! "${pr_url}" =~ ^https://github\.com/cloudnative-team/liferay-portal/pull/([0-9]+) ]]
	then
		_die "Invalid PR URL: ${pr_url}. Expected: https://github.com/cloudnative-team/liferay-portal/pull/<number>"
	fi

	PR_NUMBER="${BASH_REMATCH[1]}"
}

function _patch_application_source {
	local application="${1}"
	local source_idx="${2}"
	local repo_url="${3}"
	local target_revision="${4}"

	_log "application/${application}: sources[${source_idx}] -> ${repo_url}@${target_revision}"

	kubectl patch application "${application}" \
		--namespace "${_ARGOCD_NAMESPACE}" \
		--patch="[
			{
				\"op\": \"replace\",
				\"path\":\"/spec/sources/${source_idx}/repoURL\",
				\"value\":\"${repo_url}\"
			},
			{
				\"op\": \"replace\",
				\"path\": \"/spec/sources/${source_idx}/targetRevision\",
				\"value\":\"${target_revision}\"
			}
		]" \
		--type=json
}

function _patch_applicationset_source {
	local applicationset="${1}"
	local source_idx="${2}"
	local repo_url="${3}"
	local target_revision="${4}"

	_log "applicationset/${applicationset}: template.spec.sources[${source_idx}] -> ${repo_url}@${target_revision}"

	kubectl patch applicationset "${applicationset}" \
		--namespace "${_ARGOCD_NAMESPACE}" \
		--patch="[
			{
				\"op\": \"replace\",
				\"path\": \"/spec/template/spec/sources/${source_idx}/repoURL\",
				\"value\":\"${repo_url}\"
			},
			{
				\"op\": \"replace\", 
				\"path\": \"/spec/template/spec/sources/${source_idx}/targetRevision\",
				\"value\":\"${target_revision}\"
			}
		]" \
		--type=json
}

function _patch_appproject_source_repo {
	local appproject="${1}"
	local repo_url="${2}"

	if kubectl get appproject "${appproject}" \
		--namespace "${_ARGOCD_NAMESPACE}" \
		--output json |
		jq --arg url "${repo_url}" --exit-status '(.spec.sourceRepos // []) | index($url)' \
			> /dev/null
	then
		_log "appproject/${appproject}: sourceRepo ${repo_url} already present"
		return 0
	fi

	_log "appproject/${appproject}: adding sourceRepo ${repo_url}"

	kubectl patch appproject "${appproject}" \
		--namespace "${_ARGOCD_NAMESPACE}" \
		--patch="[
			{
				\"op\": \"add\",
				\"path\":\"/spec/sourceRepos/-\",
				\"value\":\"${repo_url}\"
			}
		]" \
		--type=json
}

function _verify_ghcr_tag {
	local chart_name="${1}"
	local tag="${2}"

	local repo="cloudnative-team/charts-pr/${PR_NUMBER}/${chart_name}"

	local token

	token=$(
		curl --fail --silent --show-error \
			"https://ghcr.io/token?service=ghcr.io&scope=repository:${repo}:pull" |
		jq --raw-output .token
	)

	local code

	code=$(
		curl --silent \
			--header "Accept: application/vnd.oci.image.manifest.v1+json" \
			--header "Authorization: Bearer ${token}" \
			--output /dev/null \
			--request HEAD \
			--write-out "%{http_code}" \
			"https://ghcr.io/v2/${repo}/manifests/${tag}"
	)

	if [[ "${code}" != "200" ]]
	then
		_die "Tag ${tag} not found on GHCR for ${chart_name} (HTTP ${code}). The publish workflow may not have finished, or git rev-parse --short HEAD (${_SHORT_SHA}) may use a different abbrev length than the GHA used."
	fi
}

function _resolve_chart_version {
	local chart_dir="${1}"

	local chart_yaml="${_SCRIPT_DIR}/../helm/${chart_dir}/Chart.yaml"

	if [[ ! -f "${chart_yaml}" ]]
	then
		_die "Chart.yaml not found at ${chart_yaml}. Run this script from a liferay-portal checkout."
	fi

	local base_version

	base_version=$(awk '/^version:[[:space:]]/ {gsub(/"|'\''/, "", $2); print $2; exit}' "${chart_yaml}")

	if [[ -z "${base_version}" ]]
	then
		_die "Could not read .version from ${chart_yaml}."
	fi

	local tag="${base_version}-pr-${PR_NUMBER}-${_SHORT_SHA}"

	_verify_ghcr_tag "liferay-${chart_dir}" "${tag}"

	echo "${tag}"
}

function main {
	if [[ $# -lt 2 ]]
	then
		echo "Usage: $0 <provider> <url>" >&2
		echo "Example: $0 gcp https://github.com/cloudnative-team/liferay-portal/pull/125" >&2
		echo "Valid providers: aws, gcp" >&2
		exit 1
	fi

	local provider="${1}"

	case "${provider}" in
		aws|gcp) ;;
		*) _die "Unsupported provider '${provider}'. Valid providers: aws, gcp." ;;
	esac

	for cmd in awk curl git jq kubectl
	do
		command -v "${cmd}" > /dev/null || _die "Required command '${cmd}' is not on PATH."
	done

	_SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

	_parse_pr_url "${2}"

	_log "Resolving PR cloudnative-team/liferay-portal#${PR_NUMBER} (provider: ${provider}, sha: ${_SHORT_SHA})"

	local provider_infra_provider_version

	provider_infra_provider_version=$(_resolve_chart_version "${provider}-infrastructure-provider")

	local provider_infra_version

	provider_infra_version=$(_resolve_chart_version "${provider}-infrastructure")

	local provider_version

	provider_version=$(_resolve_chart_version "${provider}")

	_log "$(printf '%-40s: %s\n' "liferay-${provider}" "${provider_version}")"
	_log "$(printf '%-40s: %s\n' "liferay-${provider}-infrastructure" "${provider_infra_version}")"
	_log "$(printf '%-40s: %s\n' "liferay-${provider}-infrastructure-provider" "${provider_infra_provider_version}")"

	local registry="oci://ghcr.io/cloudnative-team/charts-pr/${PR_NUMBER}"
	local provider_infra_repo="${registry}/liferay-${provider}-infrastructure"
	local provider_infra_provider_repo="${registry}/liferay-${provider}-infrastructure-provider"
	local provider_repo="${registry}/liferay-${provider}"

	local appproject
	local repo

	for appproject in liferay-application liferay-infrastructure
	do
		for repo in \
			"${provider_infra_provider_repo}" \
			"${provider_infra_provider_repo}/*" \
			"${provider_infra_repo}" \
			"${provider_infra_repo}/*" \
			"${provider_repo}" \
			"${provider_repo}/*"
		do
			_patch_appproject_source_repo "${appproject}" "${repo}"
		done
	done

	_patch_application_source \
		"liferay-infrastructure-provider" \
		0 \
		"${provider_infra_provider_repo}" \
		"${provider_infra_provider_version}"

	_patch_application_source \
		"liferay-infrastructure-provider" \
		1 \
		"${provider_infra_provider_repo}" \
		"${provider_infra_provider_version}"

	_patch_applicationset_source \
		"liferay-applicationset" \
		0 \
		"${provider_repo}" \
		"${provider_version}"

	_patch_applicationset_source \
		"liferay-infrastructure-applicationset" \
		0 \
		"${provider_infra_repo}" \
		"${provider_infra_version}"

	_patch_applicationset_source \
		"liferay-resources-applicationset" \
		0 \
		"${provider_infra_repo}" \
		"${provider_infra_version}"

	_log "ArgoCD resources patched."
}

main "${@}"
