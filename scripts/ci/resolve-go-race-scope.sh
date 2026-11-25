#!/usr/bin/env bash
# Resolve whether test.yml's go-race lane can run in single-runner changed-tests
# mode (only go/**/*_test.go files changed on a pull_request) or must run the
# full 8-shard matrix. Fails closed to full on every ambiguity.
set -euo pipefail

FULL_SHARDS='[1,2,3,4,5,6,7,8]'
MAX_SCOPED_FILES=300

event_name="${GITHUB_EVENT_NAME:-}"
repo_root=""
changed_files_path=""

usage() {
	printf 'usage: resolve-go-race-scope.sh [--event NAME] [--repo-root DIR] [--changed-files FILE]\n' >&2
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--event)
		event_name="${2:-}"
		shift 2
		;;
	--repo-root)
		repo_root="${2:-}"
		shift 2
		;;
	--changed-files)
		changed_files_path="${2:-}"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage
		exit 2
		;;
	esac
done

emit_full() {
	printf 'scope=full\n'
	printf 'shards=%s\n' "${FULL_SHARDS}"
	printf 'packages=\n'
	exit 0
}

if [[ "${event_name}" != "pull_request" ]]; then
	emit_full
fi

if [[ -z "${repo_root}" ]]; then
	repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
fi
if [[ -z "${repo_root}" || ! -d "${repo_root}/go" ]]; then
	emit_full
fi

changed_raw=""
if [[ -n "${changed_files_path}" ]]; then
	[[ -r "${changed_files_path}" ]] || emit_full
	changed_raw="$(cat "${changed_files_path}")" || emit_full
else
	# On pull_request events actions/checkout@v5 (fetch-depth: 2) checks out
	# the synthetic merge commit whose first parent is the base branch and
	# second parent is the PR head. Refuse to narrow on any non-merge HEAD.
	if ! git -C "${repo_root}" rev-parse --verify --quiet HEAD^1 >/dev/null ||
		! git -C "${repo_root}" rev-parse --verify --quiet HEAD^2 >/dev/null; then
		emit_full
	fi
	changed_raw="$(git -C "${repo_root}" diff --no-renames --name-only HEAD^1 HEAD 2>/dev/null)" || emit_full
fi

file_count=0
raw_dirs=()
while IFS= read -r path || [[ -n "${path}" ]]; do
	[[ -n "${path}" ]] || continue
	file_count=$((file_count + 1))
	if [[ "${file_count}" -gt "${MAX_SCOPED_FILES}" ]]; then
		emit_full
	fi
	# Allow only Go test files under go/ with safe path characters and no "..",
	# testdata, dot-prefixed, or underscore-prefixed segments (which `go list`
	# ignores as non-packages / fixtures).
	if [[ ! "${path}" =~ ^go/[A-Za-z0-9._/-]+_test\.go$ ]]; then
		emit_full
	fi
	case "/${path}/" in
	*/../* | */./* | *//* | */testdata/* | */_*/* | */.*/*)
		emit_full
		;;
	esac
	rel="${path#go/}"
	if [[ "${rel}" == */* ]]; then
		dir="${rel%/*}"
		pkg="./${dir}"
	else
		dir="."
		pkg="."
	fi
	# Fail closed if the package directory no longer exists or has no .go files
	# (e.g., a PR deleting a test-only package).
	target_dir="${repo_root}/go/${dir}"
	if [[ "${dir}" == "." ]]; then
		target_dir="${repo_root}/go"
	fi
	if [[ ! -d "${target_dir}" ]]; then
		emit_full
	fi
	has_go=0
	shopt -s nullglob
	go_files=("${target_dir}"/*.go)
	shopt -u nullglob
	if [[ "${#go_files[@]}" -gt 0 ]]; then
		has_go=1
	fi
	if [[ "${has_go}" -ne 1 ]]; then
		emit_full
	fi
	raw_dirs+=("${pkg}")
done < <(printf '%s\n' "${changed_raw}")

if [[ "${file_count}" -eq 0 || "${#raw_dirs[@]}" -eq 0 ]]; then
	emit_full
fi

pkgs=()
while IFS= read -r pkg; do
	[[ -n "${pkg}" ]] && pkgs+=("${pkg}")
done < <(printf '%s\n' "${raw_dirs[@]}" | LC_ALL=C sort -u)

if [[ "${#pkgs[@]}" -eq 0 ]]; then
	emit_full
fi

printf 'scope=changed-tests\n'
printf 'shards=[1]\n'
printf 'packages=%s\n' "${pkgs[*]}"
