#!/usr/bin/env bash

# Guards against the per-project overlay bucket name formula drifting between
# the places that compute it. The same formula is implemented three times:
#
#   - cloud/helm/aws/templates/_helpers.tpl
#   - cloud/helm/aws-infrastructure-provider/.../compositions.yaml (2x)
#
# The consumer chart derives the bucket name to mount, and the compositions
# derive the name they provision / expose in the overlay-bucket-details secret.
# If they disagree, pods point at a bucket that does not exist. This test
# renders each formula (Crossplane uses the same sprig functions as Helm)
# for fixed inputs and asserts they all produce the same value.

set -o errexit
set -o nounset
set -o pipefail

_ACCOUNT_ID="0123456789"
_COMPOSITIONS="templates/compositions.yaml"
_DEPLOYMENT_NAME="demo"
_HELPERS="../aws/templates/_helpers.tpl"
_PROJECT_ID="able"

_HASH="$(printf '%s-%s-%s' "${_ACCOUNT_ID}" "${_DEPLOYMENT_NAME}" "${_PROJECT_ID}" | sha256sum | cut -c1-6)"

_GOLDEN="${_DEPLOYMENT_NAME}-overlay-${_PROJECT_ID:0:18}-${_HASH}"

_SYNC_HINT="Keep the overlay bucket name formula identical in:
  - ${_COMPOSITIONS} (LiferayOverlay and LiferayInfrastructure compositions)
  - ${_HELPERS} (liferay-aws.overlayBucketName)"

_FAIL=0

function _check {
	local name="${1}"
	local actual="${2}"

	if [ "${actual}" = "${_GOLDEN}" ]
	then
		echo "PASS: ${name} produces ${actual}"
	else
		echo "FAIL: ${name} produces '${actual}' (expected '${_GOLDEN}')."

		echo "${_SYNC_HINT}"

		_FAIL=1
	fi
}

function _eval {
	local body="${1}"
	local dir

	dir="$(mktemp -d)"

	mkdir -p "${dir}/templates"

	printf 'apiVersion: v2\nname: overlay-formula-eval\nversion: 0.0.0\n' > "${dir}/Chart.yaml"

	{
		printf '{{- $accountId := "%s" -}}\n' "${_ACCOUNT_ID}"
		printf '{{- $deploymentName := "%s" -}}\n' "${_DEPLOYMENT_NAME}"
		printf '{{- $projectId := "%s" -}}\n' "${_PROJECT_ID}"
		printf '# overlay-bucket-name:%s\n' "${body}"
	} > "${dir}/templates/result.yaml"

	helm template x "${dir}" | sed -n 's/^# overlay-bucket-name://p' | tr -d '[:space:]'

	rm --force --recursive "${dir}"
}

function main {
	local helper_body

	helper_body="$(sed -n '/\$hash := printf/,/printf "%s-overlay-/p' "${_HELPERS}")"

	if [ -z "${helper_body}" ]
	then
		echo "FAIL: could not extract the overlay bucket name formula from ${_HELPERS}."

		echo "${_SYNC_HINT}"

		_FAIL=1
	else
		_check "helper (${_HELPERS})" "$(_eval "${helper_body}")"
	fi

	local composition_blocks

	composition_blocks="$(grep -B2 '\$overlayBucketName := printf "%s-overlay-%s" \$deploymentName \$overlayBaseName' "${_COMPOSITIONS}" || true)"

	if [ -z "${composition_blocks}" ]
	then
		echo "FAIL: could not extract the overlay bucket name formula from ${_COMPOSITIONS}."

		echo "${_SYNC_HINT}"

		_FAIL=1
	else
		local block=""

		while IFS= read -r line
		do
			if [ "${line}" = "--" ]
			then
				_check "composition (${_COMPOSITIONS})" "$(_eval "${block}"$'\n'"{{ \$overlayBucketName }}")"

				block=""
			else
				block="${block}${line}"$'\n'
			fi
		done < <(printf '%s\n--\n' "${composition_blocks}")
	fi

	if [ "${_FAIL}" -eq 0 ]
	then
		echo "Overlay bucket name formula is in sync (${_GOLDEN})."
	fi

	exit "${_FAIL}"
}

main