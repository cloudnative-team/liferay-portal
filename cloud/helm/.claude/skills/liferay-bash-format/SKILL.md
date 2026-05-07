---
name: liferay-bash-format
description: Apply the Liferay (liferay-docker) bash style — Brian Chan's "SF" (Source Formatting) conventions — to shell scripts. Use when editing or creating .sh files anywhere under a Liferay repo (liferay-docker, narwhal, release scripts, templates/.../bin), when the user asks to "format", "SF", "source-format", or "clean up" bash, or when reviewing a PR for Liferay bash style. The rules below are derived from the current state of liferay-docker plus 250+ commits of Brian Chan's SF/Wordsmith/Better-SF history; the current repo state always wins on conflicts.
---

# Liferay bash formatting (Brian Chan "SF" style)

Apply these rules to any `.sh` file in a Liferay repo. The current state of `liferay-docker/_common.sh`, `_liferay_common.sh`, `_release_common.sh`, and the files under `release/` are canonical references — when in doubt, mimic them.

When you are asked to format/SF/clean up a script, also re-sort and re-group; do not just patch local edits. When you are *editing* a script in the course of other work, conform to the rules but do not re-sort the whole file unless asked.

## 1. File top

- Every `.sh` file (executable or sourced) starts with `#!/bin/bash` on line 1.
- One blank line, then `source` lines, then one blank line, then function definitions.
- `source` lines are grouped: parent-directory sources (`source ../...`) first, then current-directory sources (`source ./...`); each group sorted alphabetically by path.
- No `set -e`, `set -u`, `set -o pipefail`. Errors are handled with explicit `${?}` checks, `|| exit N`, or by returning `${LIFERAY_COMMON_EXIT_CODE_BAD}`.

```bash
#!/bin/bash

source ../_liferay_common.sh
source ../_release_common.sh
source ./_bom.sh
source ./_git.sh
```

## 2. Indentation and whitespace

- **Tabs** for indentation, one tab per nesting level. Never spaces.
- For continuation lines that need column alignment (e.g. multi-line `if` conditions), use a tab to reach the function-body indent, then **spaces** to align under the first `[`. Brian's standard is `tab + 3 spaces` so the second `[` lines up under the first `[`:
  ```bash
  if [ -e "${file_name}" ] &&
     [[ "${file_url}" != */apache-tomcat/* ]] &&
     [[ "${file_url}" != */latest/* ]]
  then
  ```
- Single blank lines separate distinct logical operations. **Never two blank lines in a row.**
- No trailing whitespace.
- A single trailing blank line at end of file is acceptable; do not add a second.

## 3. Variables and quoting

- Always brace variable references: `${var}`, `${1}`, `${@}`, `${#}`, `${?}`. Never `$var` or `$1`.
- Always double-quote variable expansions in commands and tests: `"${var}"`. Exceptions: arithmetic `$(( ))` and intentional glob/regex matches inside `[[ ]]`.
- Use double quotes for string literals. Use single quotes only when needed to suppress expansion (e.g., regex inside `sed`, `grep`).
- Use `$(...)`, never backticks `` `...` ``.
- Use `local` for every function-scoped variable. Even loop variables that come from `read -r` should be predeclared:
  ```bash
  local image_name

  while read -r image_name
  do
      ...
  done
  ```
- To check `${?}` reliably (Brian works around shellcheck SC2155 manually since it is disabled), declare and assign on separate lines, blank-line separated:
  ```bash
  local http_code

  http_code=$(curl ... --write-out "%{http_code}")

  if [ "${?}" -gt 0 ]
  then
      ...
  fi
  ```

## 4. `local` declaration ordering

Within a function:

- Group `local` declarations near the top of the function, or near the first use site for declarations that belong to a specific block.
- **Sort alphabetically** within each group (case-sensitive ASCII; underscore `_` sorts after uppercase letters and before lowercase).
- Pack simple/static declarations together with no blank lines between them. Insert a blank line before a declaration that uses a multi-line command substitution or that you want to emphasize.
- If a variable is only used by one block, declare it just before that block, not at the top.

```bash
function reference_new_releases {
    local base_url="http://mirrors.lax.liferay.com/releases.liferay.com"

    local latest_quarterly_release="false"

    local product_group_version=$(echo "${_PRODUCT_VERSION}" | cut -d '.' -f 1,2)

    local previous_product_version="$(\
        grep ... | \
        tail -1 | \
        cut -d '=' -f 2)"
    ...
}
```

## 5. Function definitions

- Define with the `function` keyword and brace on the same line: `function name {`.
- Closing `}` on its own line, no trailing comment.
- One blank line between consecutive function definitions.
- **Sort all functions alphabetically** by name.
- Functions whose name starts with `_` (private helpers) come **after** all public functions, also sorted alphabetically among themselves.
- For sourced library files (`_liferay_common.sh`, etc.), the `lc_*` public API comes first alphabetically, then the `_lc_*` private helpers.

```bash
function build_docker_image {
    ...
}

function main {
    ...
}

function _internal_helper {
    ...
}
```

## 6. Control structures

`if`/`then` and `elif`/`then` always go on separate lines. Same for `for`/`do`, `while`/`do`. Never use `; then` or `; do`.

```bash
if [ -z "${value}" ]
then
    echo "Empty."

    exit 1
fi

for util in "${@}"
do
    ...
done
```

Prefer `[[ ]]` for glob/regex (`==` with glob, `=~`); use `[ ]` for simple equality/file tests.

Prefer **early return** over `if / else / return` symmetry. Do not write:

```bash
if [[ "${1}" == *q* ]]
then
    return 0
else
    return 1
fi
```

Write:

```bash
if [[ "${1}" == *q* ]]
then
    return 0
fi

return 1
```

Compound conditions:

- Short ones on one line: `[ -n "${a}" ] && [ -n "${b}" ]`.
- Long ones across lines with `&&`/`||` at end of line, continuation lines aligned under the first `[`:
  ```bash
  if [ -e "${file}" ] &&
     [[ "${url}" != */latest/* ]] &&
     [[ "${url}" != */nightly/* ]]
  then
      ...
  fi
  ```
- When two function calls are joined with `&&`/`||` and have no order dependency, put them in alphabetical order: `is_dxp_release && is_release_candidate`, not `is_release_candidate && is_dxp_release`.

`case` arms: each arm's commands are blank-line separated from `;;`, and `;;` sits on its own line at command indent. Put `*)` last.

```bash
case ${1} in
    -c)
        shift

        CONTENT=${1}

        ;;
    -d)
        shift

        DOMAIN=${1}

        ;;
    *)
        print_help

        ;;
esac
```

## 7. Commands and flags

- Prefer **GNU long-form options**:
  - `rm --force --recursive` (not `rm -fr`)
  - `mkdir --parents` (not `mkdir -p`)
  - `cp --archive` (not `cp -a`); `cp --recursive` (not `cp -r`)
  - `ln --symbolic` (not `ln -s`)
  - `sed --in-place`, `sed --expression`, `sed --regexp-extended`
  - `grep --quiet`, `grep --extended-regexp`, `grep --perl-regexp`, `grep --invert-match`, `grep --only-matching`, `grep --count`, `grep --word-regexp`, `grep --fixed-strings`, `grep --ignore-case`
  - `cut --delimiter='X' --fields=N`
  - `sort --version-sort`
  - `tail --lines=N`, `head --lines=N`
  - `tr --delete`
  - `paste --delimiters=',' --serial`
  - `xargs --max-args=1`
  - `find ... -name ... -type f`
  - For `curl`: `--fail`, `--location`, `--max-time`, `--output`, `--retry`, `--retry-delay`, `--show-error`, `--silent`, `--url`, `--write-out`
- Sort multi-flag invocations: short flags before long flags; alphabetical within each group. Then positional arguments.
- Inside `awk`, no spaces inside braces: `awk '{print $1}'`, never `awk '{ print $1 }'`.

## 8. Multi-line commands

Use trailing `\` for line continuation. Indent continuation by one extra tab.

Long `curl`/`gh`/`docker` commands: one flag per line, alphabetically sorted, value on the same line as the flag.

```bash
curl "${file_url}" \
    --fail \
    --max-time "${LIFERAY_COMMON_DOWNLOAD_MAX_TIME}" \
    --output "${cache_file}.${temp_suffix}" \
    --show-error \
    --silent \
    --write-out "%{http_code}"
```

Pipe chains: `|` at end of line, `\` at end:

```bash
git log "${range}" --pretty="%s %H" | \
    sed --expression "s/.../...g" | \
    grep --extended-regexp "..." | \
    sort | \
    uniq | \
    paste --delimiters=',' --serial > "${out}"
```

Multi-line `for ... in`: one item per line, indented one tab from the `for`:

```bash
for file_name in \
    "2024-05-17-dxp-7.4.10.6" \
    "2025-11-08-dxp-2025.q1.2" \
    "2026-12-08-dxp-2026.q4.0"
do
    touch "./test_release_json_dir/${file_name}.json"
done
```

## 9. Comments

Block / documentation comments are wrapped in lone `#` lines, with `# text` (space after `#`) for body lines:

```bash
#
# Warm up Tomcat for older versions to speed up starting Tomcat. Populating
# the Hypersonic files can take over 20 seconds.
#
```

When a function opens with such a block comment, leave one blank line between `function name {` and the opening `#`:

```bash
function warm_up_tomcat {

    #
    # Warm up Tomcat for older versions...
    #

    ...
}
```

**Commented-out code uses no space after `#`** so it visually differs from prose comments:

```bash
#return "${LIFERAY_COMMON_EXIT_CODE_SKIPPED}"
#rm --force "${name}-${version}.jar"
#lc_time_run prepare_next_release_branch
```

Avoid trailing inline comments.

## 10. Echo, log, and help text

- Sentence-style with a capital first letter and a trailing period for log/echo lines that are full sentences:
  ```bash
  echo "Building Docker image Base."
  lc_log INFO "Using JDK ${jdk_version} for release ${_PRODUCT_VERSION}."
  lc_log ERROR "Unable to download ${file_url}."
  ```
- `echo ""` for a blank output line. Don't use `echo -e` unless you actually need an escape.
- Help text: print the usage one-liner, a blank line, a description line, a blank line, then **alphabetically sorted** argument or environment-variable rows formatted as `    NAME (required|optional): Description`. Descriptions in the env-var help block do not end with a period.
  ```bash
  function print_help {
      echo "Usage: LIFERAY_RELEASE_GIT_REF=<git sha> ${0}"
      echo ""
      echo "The script reads the following environment variables:"
      echo ""
      echo "    LIFERAY_RELEASE_GCS_TOKEN (optional): *.json file containing the token..."
      echo "    LIFERAY_RELEASE_GIT_REF: Git SHA to build from"
      echo "    LIFERAY_RELEASE_HOTFIX_BUILD_ID (optional): Build ID on Patcher"
      echo ""
      echo "Example: LIFERAY_RELEASE_GIT_REF=release-2023.q3 ${0}"

      exit "${LIFERAY_COMMON_EXIT_CODE_HELP}"
  }
  ```

## 11. Naming

- Local variables: `lower_snake_case` (`product_version`, `exit_code`).
- Exported / global env vars: `UPPER_SNAKE_CASE` (`LIFERAY_DOCKER_REPOSITORY`).
- Module-private globals: leading underscore (`_BUILD_DIR`, `_RELEASE_ROOT_DIR`).
- Private functions: leading underscore (`_get_product_version`, `_lc_init`).
- Prefer concise, generic names. Rename `pull_request_creation_exit_code` → `exit_code`, `tag_release_created` → `created_release_tag`, etc.
- If a `local` only renames an env var, delete it and reference the env var directly:
  ```bash
  # Bad
  local curl_options="${LIFERAY_BATCH_CURL_OPTIONS}"
  curl ... ${curl_options} ...

  # Good
  curl ... "${LIFERAY_BATCH_CURL_OPTIONS}" ...
  ```

## 12. Sort order summary

Sort *case-sensitively* using the ASCII order. Sort:

- `source` statements (parent-dir group first, then current-dir group)
- top-level function definitions (public, then `_`-private)
- `export`/global assignments within a function
- `local` declarations within a group
- `echo`-printed help rows (env var or argument names)
- function-call lists in a `main` that just dispatches to test or sub-functions
- `case` patterns (typically; with `*)` always last)
- multi-line `for x in \` value lists
- shellcheck disables in `.shellcheckrc`
- compound `&&`/`||` operands when they're independent function calls

## 13. Bottom of file

Executable scripts end with `main "${@}"` (or `main` if no args used) on the last line. Sourced library files may end with an init call (e.g., `_lc_init`). Don't add unnecessary trailing blank lines; one trailing newline is fine.

```bash
main "${@}"
```

## 14. Tests (project-specific)

- Public test entry point: `test_<scope>_<unit>`. It calls a private data-driven helper `_test_<scope>_<unit>` once per case.
- Top-level `main` lists every public test alphabetically and also supports running a single test by name via `${1}`.
- Use `common_set_up` / `common_tear_down` from `_test_common.sh` and add scope-specific `set_up` / `tear_down` if needed.
- Use `assert_equals "$(function_under_test ...)" "expected"`.

## How to apply this skill

When asked to format / SF / clean up a Liferay shell script:

1. Read the file in full.
2. Walk the rules above top-to-bottom and produce a single edit (or set of edits) that applies all that are violated. Order changes so that intermediate states still parse — e.g., sort `local` declarations before refactoring code that uses them.
3. Re-sort: function order, `source` order, `local` groups, help-text rows, multi-line `for` value lists, `&&`/`||` compound operands, `.shellcheckrc` rules.
4. Tighten: collapse double blank lines, drop trailing whitespace, brace bare `$var`, quote bare `${var}` in commands, switch short flags to GNU long forms, remove `else: return` symmetry.
5. Wordsmith: log/echo lines as full sentences ending with a period; help-row descriptions without trailing period; concise variable names.
6. After editing, scan once more for double blank lines, unbraced `$var`, short flags, and `if ... ; then` patterns — these are the most common slips.

When in doubt about a specific construct, search the current `liferay-docker` tree for a similar existing pattern (`grep` in `_common.sh`, `_liferay_common.sh`, `release/_*.sh`, `release/build_release.sh`) and copy that shape verbatim. The current repo state always wins over any rule above.
