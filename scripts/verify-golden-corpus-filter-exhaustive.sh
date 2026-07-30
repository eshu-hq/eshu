#!/usr/bin/env bash
# B-7 golden-corpus PR path filter EXHAUSTIVENESS gate (#5538 task 4, the
# root-cause fix). Three successive review rounds (#5596, 6a6944fbad,
# 1de212bd6e) each found MORE go/internal/* or go/cmd/* packages missing from
# golden-corpus-gate.yml's on.pull_request.paths filter, because completeness
# was maintained by hand -- see scripts/lib/golden-corpus-mirror-workflow-
# paths.sh's header for the incident history. This script makes it converge:
# every package actually compiled into the six binaries
# scripts/verify-golden-corpus-gate.sh builds (bootstrap-index, projector,
# reducer, api, mcp-server, golden-corpus-gate) must be EITHER
#
#   1. covered by a path in the workflow's on.pull_request.paths filter, OR
#   2. listed with a one-line reason in
#      scripts/lib/golden-corpus-filter-exclusions.txt
#
# A package in NEITHER set fails here, loudly, naming the exact package --
# instead of silently shipping an unverified change until the unconditional
# push-to-main trigger catches it post-merge (the wrong side of the review
# boundary). Adding a package anywhere in the six binaries' import graph now
# forces an explicit classify-or-cover decision instead of relying on the next
# reviewer to notice.
#
# `go list -deps` only -- no `go build`, no Docker, no credentials. Fast
# enough to run on every PR.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_dir="${repo_root}/go"
module_prefix="github.com/eshu-hq/eshu/go"

# Overridable for the test mirror's negative cases; production callers should
# never set these.
workflow="${GOLDEN_CORPUS_FILTER_WORKFLOW:-${repo_root}/.github/workflows/golden-corpus-gate.yml}"
exclusions="${GOLDEN_CORPUS_FILTER_EXCLUSIONS:-${repo_root}/scripts/lib/golden-corpus-filter-exclusions.txt}"

fail() { printf 'verify-golden-corpus-filter-exhaustive: %s\n' "$*" >&2; exit 1; }

[[ -f "${workflow}" ]] || fail "missing workflow: ${workflow}"
[[ -f "${exclusions}" ]] || fail "missing exclusions file: ${exclusions}"
command -v yq >/dev/null 2>&1 || fail "yq (mikefarah/yq v4) is required"
command -v go >/dev/null 2>&1 || fail "go is required"
command -v rg >/dev/null 2>&1 || fail "rg (ripgrep) is required"

# The six binaries scripts/verify-golden-corpus-gate.sh actually builds.
binaries=(bootstrap-index projector reducer api mcp-server golden-corpus-gate)

# --- 1. derive the compiled package set -------------------------------------
compiled_pkgs_raw="$(mktemp -t golden-corpus-filter-exhaustive.XXXXXX)"
trap 'rm -f "${compiled_pkgs_raw}"' EXIT

for bin in "${binaries[@]}"; do
	cmd_dir="${go_dir}/cmd/${bin}"
	[[ -d "${cmd_dir}" ]] || fail "missing cmd package for binary ${bin}: ${cmd_dir}"
	(cd "${go_dir}" && go list -deps "./cmd/${bin}/...") >>"${compiled_pkgs_raw}" \
		|| fail "go list -deps failed for ./cmd/${bin}/..."
done

# Normalize every import path to its top-level go/internal/X or go/cmd/X
# component -- the granularity both the workflow filter (a "**" glob per
# top-level dir) and the exclusions file use.
compiled_pkgs="$(
	rg --only-matching --no-filename \
		"^${module_prefix}/(internal|cmd)/[^/]+" "${compiled_pkgs_raw}" \
		| sed -E "s#^${module_prefix}/#go/#" \
		| sort -u
)"
[[ -n "${compiled_pkgs}" ]] || fail "derived an empty compiled-package set; go list output looks wrong"

# --- 2. load the workflow's path filter --------------------------------------
workflow_paths="$(yq e '.on.pull_request.paths[]' "${workflow}")"
[[ -n "${workflow_paths}" ]] || fail "workflow filter parsed to zero paths: ${workflow}"

# path_covers: does one on.pull_request.paths glob cover a package path? Only
# two glob shapes are actually used by this filter today:
#   "go/internal/foo/**"  -> matches "go/internal/foo" itself and anything
#                            nested under it.
#   "go/cmd/collector-**" -> matches anything starting with the literal
#                            prefix before "**" (no separating slash).
# Any other literal entry (e.g. "testdata/golden/**") simply never matches a
# go/internal or go/cmd package path and is harmless to compare against.
path_covers() {
	local pkg="$1" pattern="$2" prefix bare
	case "${pattern}" in
	*'**')
		prefix="${pattern%'**'}"
		bare="${prefix%/}"
		[[ "${pkg}" == "${bare}" || "${pkg}" == "${prefix}"* ]]
		;;
	*)
		[[ "${pkg}" == "${pattern}" ]]
		;;
	esac
}

is_covered() {
	local pkg="$1" pattern
	while IFS= read -r pattern; do
		[[ -n "${pattern}" ]] || continue
		path_covers "${pkg}" "${pattern}" && return 0
	done <<<"${workflow_paths}"
	return 1
}

# --- 3. load the exclusions file --------------------------------------------
# "NOT REACHABLE:" is a machine-readable marker (not decoration -- see step
# 3b below): a reason carrying it asserts the package is absent from every
# compiled binary's `go list -deps` output today. That claim must stay
# self-enforcing, because the classify loop in step 4 only ever inspects
# packages already IN the compiled set -- a NOT-REACHABLE package that later
# becomes reachable would otherwise be silently classified as "excluded"
# forever, the same class of drift #5877 found in a plain (non-marked)
# exclusion entry.
not_reachable_marker='NOT REACHABLE:'
declare -A excluded_reason=()
declare -A not_reachable_pkgs=()
while IFS= read -r line; do
	[[ -n "${line}" && "${line}" != '#'* ]] || continue
	excl_pkg=""
	excl_reason=""
	read -r excl_pkg excl_reason <<<"${line}"
	[[ -n "${excl_pkg}" ]] || continue
	[[ -n "${excl_reason}" ]] || fail "exclusion entry for ${excl_pkg} carries no reason: ${exclusions}"
	excluded_reason["${excl_pkg}"]="${excl_reason}"
	if [[ "${excl_reason}" == "${not_reachable_marker}"* ]]; then
		not_reachable_pkgs["${excl_pkg}"]=1
	fi
done <"${exclusions}"
[[ "${#excluded_reason[@]}" -gt 0 ]] || fail "exclusions file parsed to zero entries: ${exclusions}"

# --- 3b. NOT-REACHABLE convergence check -------------------------------------
# Independent of step 4's classification: a NOT-REACHABLE package that shows
# up in the compiled set fails here even though it also has a (now stale)
# excluded_reason entry, which step 4 alone would accept silently.
declare -A compiled_set=()
while IFS= read -r pkg; do
	[[ -n "${pkg}" ]] || continue
	compiled_set["${pkg}"]=1
done <<<"${compiled_pkgs}"

now_reachable=()
for pkg in "${!not_reachable_pkgs[@]}"; do
	[[ -n "${compiled_set[${pkg}]:-}" ]] || continue
	now_reachable+=("${pkg}")
done
if [[ "${#now_reachable[@]}" -gt 0 ]]; then
	# Associative-array key iteration order is not guaranteed stable; sort so
	# the failure message (and case_deterministic, should this path ever be
	# exercised by a real regression) is byte-identical across runs.
	mapfile -t now_reachable < <(printf '%s\n' "${now_reachable[@]}" | sort -u)
	{
		printf 'FAIL: %d package(s) marked "NOT REACHABLE" in the exclusions file are now compiled into the golden-corpus-gate binaries:\n' "${#now_reachable[@]}"
		printf '  %s\n' "${now_reachable[@]}"
		printf 'Fix: this package is no longer unreachable -- either cover it (add a path entry to %s, mirror it in specs/ci-gates.v1.yaml, and assert it in scripts/lib/golden-corpus-mirror-workflow-paths.sh), OR replace its "NOT REACHABLE:" reason in %s with a real reason for why a change to it still cannot alter what the B-7 gate projects or asserts.\n' \
			"${workflow#"${repo_root}/"}" "${exclusions#"${repo_root}/"}"
	} >&2
	exit 1
fi

# --- 4. classify every compiled package --------------------------------------
uncovered=()
covered_count=0
excluded_count=0
while IFS= read -r pkg; do
	[[ -n "${pkg}" ]] || continue
	if is_covered "${pkg}"; then
		covered_count=$((covered_count + 1))
		continue
	fi
	if [[ -n "${excluded_reason[${pkg}]:-}" ]]; then
		excluded_count=$((excluded_count + 1))
		continue
	fi
	uncovered+=("${pkg}")
done <<<"${compiled_pkgs}"

if [[ "${#uncovered[@]}" -gt 0 ]]; then
	{
		printf 'FAIL: %d package(s) compiled into the golden-corpus-gate binaries are neither covered by the workflow path filter nor listed in the exclusions file:\n' "${#uncovered[@]}"
		printf '  %s\n' "${uncovered[@]}"
		printf 'Fix: add a path entry to %s (mirror it in specs/ci-gates.v1.yaml and assert it in scripts/lib/golden-corpus-mirror-workflow-paths.sh), OR add a one-line reason to %s.\n' \
			"${workflow#"${repo_root}/"}" "${exclusions#"${repo_root}/"}"
	} >&2
	exit 1
fi

compiled_count="$(wc -l <<<"${compiled_pkgs}" | tr -d '[:space:]')"
printf 'PASS: golden-corpus filter exhaustiveness: %s compiled package(s), %s covered by the workflow filter, %s explicitly excluded, 0 uncovered\n' \
	"${compiled_count}" "${covered_count}" "${excluded_count}"
