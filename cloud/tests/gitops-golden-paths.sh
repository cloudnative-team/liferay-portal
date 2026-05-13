#!/usr/bin/env bash
#
# gitops-golden-paths.sh
#
# For each case in testspec.yaml, render the chart the way an ArgoCD
# Application/ApplicationSet from cloud/terraform/gcp/gitops/resources/argocd.tf
# would render it -- gitops-repo values files first, then helm.parameters as
# --set overrides -- and diff the result against the expected output checked
# into cloud/tests/expected-outputs/.
#
# Usage:
# ./gitops-golden-paths.sh # run every case, diff vs golden
# ./gitops-golden-paths.sh --update # overwrite golden files
# ./gitops-golden-paths.sh --list # print case names
# ./gitops-golden-paths.sh <name>... # run only the named cases

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/../.." && pwd)
testspec="$script_dir/testspec.yaml"
golden_dir="$script_dir/expected-outputs"

for cmd in helm yq diff; do
	command -v "$cmd" >/dev/null || { echo "missing required command: $cmd" >&2; exit 127; }
done

[[ -f $testspec ]] || { echo "testspec not found: $testspec" >&2; exit 1; }

update=0
filter=()
while (( $# )); do
	case $1 in
		--update) update=1 ;;
		--list)
			yq -r '.tests[].name' "$testspec"
			exit 0
			;;
		-h|--help)
			sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
			exit 0
			;;
		-*) echo "unknown flag: $1" >&2; exit 2 ;;
		*) filter+=("$1") ;;
	esac
	shift
done

mkdir -p "$golden_dir"

mapfile -t redactions < <(yq -r '.redactions[]?' "$testspec" 2>/dev/null)

apply_redactions() {
	if (( ${#redactions[@]} == 0 )); then
		cat
		return
	fi
	local sed_args=()
	for r in "${redactions[@]}"; do
		sed_args+=(-e "$r")
	done
	sed -E "${sed_args[@]}"
}

# Ensure each chart's dependencies are present before templating.
ensure_deps() {
	local chart_dir=$1
	[[ -f $chart_dir/Chart.yaml ]] || return 0
	if yq -e '.dependencies // [] | length > 0' "$chart_dir/Chart.yaml" >/dev/null 2>&1; then
		if [[ ! -d $chart_dir/charts ]] || [[ -z $(ls -A "$chart_dir/charts" 2>/dev/null) ]]; then
			echo "  helm dependency build $chart_dir"
			helm dependency build "$chart_dir" >/dev/null
		fi
	fi
}

case_count=$(yq -r '.tests | length' "$testspec")
fail=0
ran=0

for i in $(seq 0 $((case_count - 1))); do
	name=$(yq -r ".tests[$i].name" "$testspec")

	if (( ${#filter[@]} )); then
		match=0
		for f in "${filter[@]}"; do [[ $f == "$name" ]] && match=1; done
		(( match )) || continue
	fi

	chart=$(yq -r ".tests[$i].chart" "$testspec")
	release=$(yq -r ".tests[$i].releaseName" "$testspec")
	namespace=$(yq -r ".tests[$i].namespace // \"default\"" "$testspec")
	chart_abs="$repo_root/$chart"
	golden="$golden_dir/$name.yaml"

	if [[ ! -d $chart_abs ]]; then
		echo "FAIL $name (chart not found: $chart_abs)"
		fail=1
		continue
	fi

	ensure_deps "$chart_abs"

	helm_args=(template "$release" "$chart_abs" --namespace "$namespace")

	while IFS= read -r vf; do
		[[ -z $vf ]] && continue
		helm_args+=(--values "$script_dir/$vf")
	done < <(yq -r ".tests[$i].valueFiles[]?" "$testspec")

	while IFS= read -r kv; do
		[[ -z $kv ]] && continue
		helm_args+=(--set "$kv")
	done < <(yq -r ".tests[$i].parameters // {} | to_entries | .[] | \"\(.key)=\(.value)\"" "$testspec")

	if ! actual=$(helm "${helm_args[@]}" 2>&1); then
		echo "FAIL $name (helm template error)"
		printf '%s\n' "$actual" | sed 's/^/    /'
		fail=1
		ran=$((ran + 1))
		continue
	fi

	actual=$(printf '%s\n' "$actual" | apply_redactions)

	if (( update )); then
		printf '%s\n' "$actual" > "$golden"
		echo "WROTE $name -> $golden"
	elif [[ -f $golden ]]; then
		if diff -u "$golden" <(printf '%s\n' "$actual") >/dev/null; then
			echo "PASS $name"
		else
			echo "FAIL $name (diff vs golden)"
			diff -u "$golden" <(printf '%s\n' "$actual") | sed 's/^/    /' | head -80
			fail=1
		fi
	else
		echo "FAIL $name (no golden at $golden -- run with --update to seed)"
		fail=1
	fi
	ran=$((ran + 1))
done

echo
if (( fail )); then
	echo "FAIL: $ran case(s) ran, at least one failure"
	exit 1
fi
echo "OK: $ran case(s) ran"