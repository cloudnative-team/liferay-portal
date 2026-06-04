#!/usr/bin/env bash

# Guards against the per-project overlay bucket name formula drifting between
# the places that compute it. The same formula is implemented twice:
#
# - compositions/00-globals.gotmpl (LiferayInfrastructure composition)
# - compositions-overlay/00-globals.gotmpl (LiferayOverlay composition)
#
# The LiferayOverlay composition provisions the bucket and the LiferayInfrastructure
# composition exposes the name in the overlay-bucket-details secret. If they
# disagree, pods point at a bucket that does not exist. This test renders each
# formula (Crossplane uses the same sprig functions as Helm) for fixed inputs and
# asserts they all produce the same value.

set -o errexit
set -o nounset
set -o pipefail

_GLOBALS=(
	"compositions/00-globals.gotmpl"
	"compositions-overlay/00-globals.gotmpl"
)
_PROJECT_ID="able"
_PROJECT_NUMBER="0123456789"

_HASH="$(printf '%s-%s' "${_PROJECT_NUMBER}" "${_PROJECT_ID}" | sha256sum | cut -c1-6)"

_GOLDEN="${_PROJECT_ID:0:18}-${_HASH}-overlay"

_SYNC_HINT="Keep the overlay bucket name formula identical in:
- compositions/00-globals.gotmpl
- compositions-overlay/00-globals.gotmpl"

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

	# Emit the value inside a YAML comment so Helm renders an (effectively empty) manifest rather
	# than choking on a bare scalar; the formula body's output lands right after the colon.
	{
		printf '{{- $env := dict "projectNumber" "%s" -}}\n' "${_PROJECT_NUMBER}"
		printf '{{- $projectId := "%s" -}}\n' "${_PROJECT_ID}"
		printf '# overlay-bucket-name:%s\n' "${body}"
	} > "${dir}/templates/result.yaml"

	helm template x "${dir}" | sed -n 's/^# overlay-bucket-name://p' | tr -d '[:space:]'

	rm --force --recursive "${dir}"
}

function main {
	local file
	local body

	for file in "${_GLOBALS[@]}"
	do
		# The formula block is the three assignment lines ending in "$overlayBucketName :=".
		body="$(grep -B2 '\$overlayBucketName := printf "%s-overlay" \$overlayBaseName' "${file}" || true)"

		if [ -z "${body}" ]
		then
			echo "FAIL: could not extract the overlay bucket name formula from ${file}."

			echo "${_SYNC_HINT}"

			_FAIL=1
		else
			_check "${file}" "$(_eval "${body}"$'\n'"{{ \$overlayBucketName }}")"
		fi
	done

	if [ "${_FAIL}" -eq 0 ]
	then
		echo "Overlay bucket name formula is in sync (${_GOLDEN})."
	fi

	exit "${_FAIL}"
}

main