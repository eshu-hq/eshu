#!/usr/bin/env bash
# Static regression tests for scripts/dev/pre-pr.sh whole-module gate scheduling.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/pre-pr.sh"
precommit_script="${repo_root}/scripts/dev/precommit-go.sh"

fail() {
	printf 'test-pre-pr-whole-module-gates: %s\n' "$*" >&2
	exit 1
}

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || \
		fail "missing ${label}: ${needle}"
}

require_precommit() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${precommit_script}" || \
		fail "missing ${label}: ${needle}"
}

require_block() {
	local label="$1" needle="$2"
	rg --multiline --fixed-strings --quiet -- "${needle}" "${script}" || \
		fail "missing ${label}: ${needle}"
}

reject() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${script}"; then
		fail "unexpected ${label}: ${needle}"
	fi
}

reject_precommit() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${precommit_script}"; then
		fail "unexpected ${label}: ${needle}"
	fi
}

[[ -f "${script}" ]] || fail "missing ${script}"
bash -n "${script}" || fail "pre-pr.sh has a syntax error"
[[ -f "${precommit_script}" ]] || fail "missing ${precommit_script}"
bash -n "${precommit_script}" || fail "precommit-go.sh has a syntax error"

cache_paths="$("${precommit_script}" cache-paths)" ||
	fail "precommit-go.sh cache-paths failed"
tool_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
worktree_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"
golangci_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^golangci_cache_dir=' | cut -d= -f2-)"
expected_git_common="$(git -C "${repo_root}" rev-parse --git-common-dir)"
case "${expected_git_common}" in
	/*) ;;
	*) expected_git_common="${repo_root}/${expected_git_common}" ;;
esac
expected_git_dir="$(git -C "${repo_root}" rev-parse --git-dir)"
case "${expected_git_dir}" in
	/*) ;;
	*) expected_git_dir="${repo_root}/${expected_git_dir}" ;;
esac
expected_tool_cache="${expected_git_common}/eshu-precommit"
expected_worktree_cache="${expected_git_dir}/eshu-precommit-state"

[[ "${tool_cache_dir}" == "${expected_tool_cache}" ]] ||
	fail "tool cache = ${tool_cache_dir}, want ${expected_tool_cache}"
[[ "${worktree_cache_dir}" == "${expected_worktree_cache}" ]] ||
	fail "worktree cache = ${worktree_cache_dir}, want ${expected_worktree_cache}"
[[ "${golangci_cache_dir}" == "${worktree_cache_dir}/golangci-lint" ]] ||
	fail "golangci cache = ${golangci_cache_dir}, want ${worktree_cache_dir}/golangci-lint"

temp_root="$(mktemp -d)"
trap 'rm -rf "${temp_root}"' EXIT
mini_repo="${temp_root}/repo"
linked_worktree="${temp_root}/linked"
git init -q "${mini_repo}"
canonical_mini_repo="$(git -C "${mini_repo}" rev-parse --show-toplevel)"
mkdir -p "${mini_repo}/scripts/dev" "${mini_repo}/.github/workflows"
cp "${precommit_script}" "${mini_repo}/scripts/dev/precommit-go.sh"
printf '%s\n' 'run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2' > "${mini_repo}/.github/workflows/test.yml"
printf '%s\n' 'run: go install github.com/securego/gosec/v2/cmd/gosec@v2.27.1' > "${mini_repo}/.github/workflows/security-scan.yml"
git -C "${mini_repo}" add scripts .github
git -C "${mini_repo}" -c user.name=cache-test -c user.email=cache-test@example.invalid commit -qm init
git -C "${mini_repo}" worktree add --detach -q "${linked_worktree}" HEAD

main_paths="$(cd "${mini_repo}" && scripts/dev/precommit-go.sh cache-paths)"
linked_paths="$(cd "${linked_worktree}" && scripts/dev/precommit-go.sh cache-paths)"
main_tool_cache="$(printf '%s\n' "${main_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
linked_tool_cache="$(printf '%s\n' "${linked_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
main_worktree_cache="$(printf '%s\n' "${main_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"
linked_worktree_cache="$(printf '%s\n' "${linked_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"

[[ "${main_tool_cache}" == "${linked_tool_cache}" ]] ||
	fail "linked worktrees did not share the tool cache"
[[ "${main_tool_cache}" == "${canonical_mini_repo}/.git/eshu-precommit" ]] ||
	fail "normal-checkout tool cache = ${main_tool_cache}, want ${canonical_mini_repo}/.git/eshu-precommit"
[[ "${main_worktree_cache}" == "${canonical_mini_repo}/.git/eshu-precommit-state" ]] ||
	fail "normal-checkout mutable cache = ${main_worktree_cache}, want ${canonical_mini_repo}/.git/eshu-precommit-state"
[[ "${main_tool_cache}" != "${main_worktree_cache}" ]] ||
	fail "normal checkout unexpectedly mixed tool binaries and mutable state"
[[ "${main_worktree_cache}" != "${linked_worktree_cache}" ]] ||
	fail "linked worktrees unexpectedly shared mutable precommit state"

# Keep the production consumers tied to the isolated paths. Calculating the
# right directories is insufficient if a linter or report still writes to the
# shared tool cache.
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require_precommit "worktree-local stripped config" 'local out="${worktree_cache_dir}/golangci-nocustom.yml"'
# shellcheck disable=SC2016
require_precommit "worktree-local golangci cache" 'GOLANGCI_LINT_CACHE="${golangci_cache_dir}" "$@"'
# shellcheck disable=SC2016
reject_precommit "ambient golangci cache override" 'GOLANGCI_LINT_CACHE:-'
# shellcheck disable=SC2016
require_precommit "disabled ambient golangci cache program" 'GOLANGCI_LINT_CACHEPROG='
# shellcheck disable=SC2016
parallel_run_count="$(rg --fixed-strings -c -- '--allow-parallel-runners --config "${cfg}"' "${precommit_script}")"
[[ "${parallel_run_count}" == "2" ]] ||
	fail "parallel-runner flag count = ${parallel_run_count}, want 2 lint entrypoints"
# shellcheck disable=SC2016
require_precommit "worktree-local changed-package SARIF" 'out="${worktree_cache_dir}/gosec.sarif"'
# shellcheck disable=SC2016
require_precommit "worktree-local whole-module SARIF" 'out="${worktree_cache_dir}/gosec-all.sarif"'
# shellcheck disable=SC2016
reject_precommit "mutable SARIF in shared tool cache" 'out="${tool_cache_dir}/gosec'

# #5791/#5804: the nancy case must delegate its sleuth/classification logic
# to scripts/dev/nancy-local.sh, which has its own executable regression
# suite (scripts/test-nancy-local.sh) exercising real exit codes for the
# empty-stdin, transport/auth-failure, clean-scan, genuine-finding, and
# go-list-failure cases with fake `go`/`nancy` binaries — a source-text-only
# assertion here previously let a broken pipeline (missing stdin redirect)
# pass silently (PR #5806 review), so this file only checks the delegation
# shape, not the classification logic itself.
# shellcheck disable=SC2016
require_precommit "nancy delegates to nancy-local.sh" 'bash "${repo_root}/scripts/dev/nancy-local.sh"'
# shellcheck disable=SC2016
reject_precommit "nancy no longer runs unpiped inline" '&& "${bin}" sleuth --no-color ) \'

require "serial precommit lane" "run_precommit_gates_serial()"
require "captured gate helper" "capture_whole_module_gate()"
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require "fmt capture" 'capture_whole_module_gate "${tmpdir}" fmt "gofumpt (whole module)" step_fmt'
# shellcheck disable=SC2016
require "lint capture" 'capture_whole_module_gate "${tmpdir}" lint "golangci-lint (whole module)" step_lint'
# shellcheck disable=SC2016
require "build capture" 'capture_whole_module_gate "${tmpdir}" build "go build ./..." step_build'
# shellcheck disable=SC2016
require "vet capture" 'capture_whole_module_gate "${tmpdir}" vet "go vet ./..." step_vet'
# shellcheck disable=SC2016
require "stored duration readback" 'duration="$(cat "${tmpdir}/${n}.duration" 2>/dev/null || printf "0")"'

reject "shared parallel launcher state" "starts=()"
reject "wait-time duration accounting" 'SECONDS - starts[i]'

awk '
	/^run_precommit_gates_serial\(\)/ { in_func=1 }
	in_func && /capture_whole_module_gate .* fmt / { saw_fmt=NR }
	in_func && /capture_whole_module_gate .* lint / {
		if (saw_fmt == 0) {
			print "lint is captured before fmt in run_precommit_gates_serial" > "/dev/stderr"
			exit 1
		}
		saw_lint=NR
	}
	in_func && /^}/ { in_func=0 }
	END {
		if (saw_fmt == 0 || saw_lint == 0) {
			print "run_precommit_gates_serial must capture fmt then lint" > "/dev/stderr"
			exit 1
		}
	}
' "${script}" || fail "fmt/lint are not serialized in the precommit lane"

# ─── #5721 documentation fast-path wiring ────────────────────────────────────
# The lane decision itself lives in scripts/lib/pre-pr-lane.sh and has its own
# executable suites, which drive pre_pr_decide_lane directly. What those suites
# cannot reach is the WIRING in this file: which function supplies the changed
# paths, and which lane value gates the Go lanes. Each is a one-line edit, none
# of them shows up in a green run, and together they decide whether a push was
# verified at all. Pin them here, where the blocking ci-gate-registry gate --
# which triggers on scripts/dev/pre-pr.sh -- already runs.

# The lane decision is the ONLY consumer of untracked paths: the FULL lane's
# `go build ./...` compiles a file nobody ran `git add` on, and the FAST lane
# skips that build. Drop either collector and a forgotten `git add` takes a
# green docs-only stamp on a tree that does not compile.
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require_block "untracked paths in the lane input" 'lane_input_paths() {
	{
		collect_changed_paths
		git_untracked_names
	} | sort -u
}'
# shellcheck disable=SC2016
require "lane decision delegated to the tested function" \
	'pre_pr_decide_lane "${repo_root}" "${base}" "${PRE_PR_FASTPATH_BASE_STATUS}" lane_input_paths'
# A literal verdict skips every gate on every run, forever, and no test of the
# decision function can see it because the decision function is never called.
reject "hardcoded lane verdict" 'PRE_PR_FASTPATH_LANE=fast'

# A self-check that failed has to fail the RUN, not just print a FAIL line: the
# run's exit status is what withholds the per-SHA stamp the push requires.
# shellcheck disable=SC2016
require_block "a red self-check fails the run" \
	'	results+=("FAIL  docs fast-path classifier self-check (${PRE_PR_LANE_SELFCHECK_SECONDS}s)")
	overall=1'

# Both Go-lane gates ask whether the lane is NOT "fast". Asking whether it IS
# "full" instead means any third value would skip the Go lanes while the banner
# said FULL and the run still stamped the SHA. Inverting either one swaps the
# lanes outright -- FULL skipping the gates, FAST running them.
# shellcheck disable=SC2016
require_block "module gates gated on a non-fast lane" \
	'if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then
	run_whole_module_gates_parallel
else'
# shellcheck disable=SC2016
require_block "race lane gated on a non-fast lane" \
	'if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then
	run_step "race lane (Go changes)" step_race'
# shellcheck disable=SC2016
lane_gate_count="$(rg --fixed-strings -c -- 'if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then' "${script}")"
[[ "${lane_gate_count}" == "2" ]] ||
	fail "lane gate count = ${lane_gate_count}, want 2 (whole-module gates + race lane)"
# shellcheck disable=SC2016
reject "lane gate asking whether the lane IS fast" '"${PRE_PR_FASTPATH_LANE}" == "fast"'
# shellcheck disable=SC2016
reject "lane gate asking whether the lane IS full" '"${PRE_PR_FASTPATH_LANE}" == "full"'

# eshu-hq/eshu#5935 review: `go test (changed packages)` must be reached on
# BOTH lanes, unconditionally -- its own scope (changed-Go-package dirs plus
# fixture_consumer_dirs) is what still runs TestRepositoryDocumentationStandards-
# AreEnforced for a root AGENTS.md/CLAUDE.md-only diff. A future edit that
# re-wraps the `run_step "go test (changed packages)" step_test` line back
# inside the whole-module-gates if/fi block -- restoring the shape just above,
# where it used to sit right after run_whole_module_gates_parallel -- would
# silently reopen that bug: the classifier still says FAST, but step_test (and
# the fixture_consumer_dirs mapping inside it) would only run on FULL again.
# Neither scripts/lib/test-pre-pr-lane.sh nor scripts/lib/test-pre-pr-docs-
# fastpath.sh can catch this: neither execs pre-pr.sh or step_test, both only
# drive the classifier and pre_pr_decide_lane in isolation. This is a purely
# static, line-position check -- it locates the first fast/full `if` (the
# whole-module-gates one), its matching top-level `fi`, and the one `run_step
# "go test (changed packages)" step_test` call, and asserts the call's line is
# outside the `[if, fi]` span. It cannot see whether step_test is *reachable*
# at runtime (that would need to execute pre-pr.sh, which this suite
# deliberately does not do), only whether it is textually gated by this
# specific if/fi -- which is exactly the shape the regression takes.
awk '
	/if \[\[ "\$\{PRE_PR_FASTPATH_LANE\}" != "fast" \]\]; then/ && !if_line { if_line = NR }
	if_line && !fi_line && /^fi$/ { fi_line = NR }
	/run_step "go test \(changed packages\)" step_test/ { test_count++; test_line = NR }
	END {
		if (!if_line) {
			print "could not find the whole-module-gates fast/full if" > "/dev/stderr"
			exit 1
		}
		if (!fi_line) {
			print "could not find that if'"'"'s matching fi" > "/dev/stderr"
			exit 1
		}
		if (test_count != 1) {
			print "found " test_count " `go test (changed packages)` run_step call(s), want exactly 1" > "/dev/stderr"
			exit 1
		}
		if (test_line > if_line && test_line < fi_line) {
			print "go test (changed packages) is gated INSIDE the whole-module-gates if/fi (line " test_line ", block " if_line "-" fi_line "); it must run unconditionally on both lanes" > "/dev/stderr"
			exit 1
		}
	}
' "${script}" || fail "go test (changed packages) is not reached on both lanes"

# ─── #5939 review: fixture_consumer_dirs must select ./internal/cigates for a
# registry- or workflow-only change ───────────────────────────────────────────
# TestScriptWorkflowSoundSubsetCount (go/internal/cigates) re-derives its count
# from specs/ci-gates.v1.yaml and .github/workflows/*.yml, neither a Go
# package, so a registry- or workflow-only change previously selected nothing
# in step_test. step_exactness's ci-gate-registry gate does not cover the gap:
# executeGates (go/cmd/ci-gates/main.go) runs only a gate's local.command,
# never local.test_command, which is the only place `go test ./internal/cigates`
# was wired for that gate. Without the fixture_consumer_dirs mapping, that
# guard -- and the rest of internal/cigates' real-registry tests -- would first
# run in the unconditional verify-ci-gate-registry.yml CI job instead of
# locally.
#
# This extracts the REAL fixture_consumer_dirs function body from the
# committed script and calls it with a stubbed changed_all_files, rather than
# reimplementing its logic, so a future edit that breaks the mapping (a typo
# in the regex, a reverted line) fails here instead of only in a manual repro.
fixture_consumer_fn="$(awk '
	/^fixture_consumer_dirs\(\) \{$/ { printing = 1 }
	printing { print }
	printing && /^}$/ { exit }
' "${script}")"
[[ -n "${fixture_consumer_fn}" ]] || fail "could not extract fixture_consumer_dirs from ${script}"

# run_fixture_consumer_dirs drives the extracted function against a synthetic
# single-path changed set, with no git state involved: changed_all_files is
# stubbed to echo SYNTHETIC_CHANGED_PATH instead of running collect_changed_paths.
run_fixture_consumer_dirs() {
	SYNTHETIC_CHANGED_PATH="$1" bash -c '
		changed_all_files() { printf "%s\n" "${SYNTHETIC_CHANGED_PATH}"; }
		'"${fixture_consumer_fn}"'
		fixture_consumer_dirs
	'
}

registry_only_dirs="$(run_fixture_consumer_dirs 'specs/ci-gates.v1.yaml')"
printf '%s\n' "${registry_only_dirs}" | rg --fixed-strings --quiet -- './internal/cigates' ||
	fail "a specs/ci-gates.v1.yaml-only change did not select ./internal/cigates (got: ${registry_only_dirs})"

workflow_only_dirs="$(run_fixture_consumer_dirs '.github/workflows/verify-ci-gate-registry.yml')"
printf '%s\n' "${workflow_only_dirs}" | rg --fixed-strings --quiet -- './internal/cigates' ||
	fail "a .github/workflows/-only change did not select ./internal/cigates (got: ${workflow_only_dirs})"

unrelated_dirs="$(run_fixture_consumer_dirs 'README.md')"
if printf '%s\n' "${unrelated_dirs}" | rg --fixed-strings --quiet -- './internal/cigates'; then
	fail "an unrelated change unexpectedly selected ./internal/cigates (got: ${unrelated_dirs})"
fi

# The specs/ branch must be exact-match ($-anchored), matching its
# '^(CLAUDE|AGENTS)\.md$' sibling above -- an unanchored prefix would
# over-trigger on e.g. a backup or generated sibling file that merely starts
# with the registry's name.
registry_lookalike_dirs="$(run_fixture_consumer_dirs 'specs/ci-gates.v1.yaml.bak')"
if printf '%s\n' "${registry_lookalike_dirs}" | rg --fixed-strings --quiet -- './internal/cigates'; then
	fail "an unanchored specs/ match selected ./internal/cigates for a lookalike path (got: ${registry_lookalike_dirs})"
fi

# ─── fixture_consumer_dirs branch coverage (#5721 follow-up) ────────────────
# fixture_consumer_dirs (scripts/dev/pre-pr.sh) maps two non-Go fixture changes
# to the Go package whose tests actually load them: the B-12 golden snapshot to
# ./cmd/golden-corpus-gate, and a root CLAUDE.md/AGENTS.md edit to
# ./internal/runtime (TestRepositoryDocumentationStandardsAreEnforced). Before
# this pin, nothing outside pre-pr.sh itself asserted either mapping: every
# other mention of fixture_consumer_dirs in this suite, in
# scripts/lib/test-pre-pr-lane.sh, and in pre-pr-lane.sh/pre-pr-docs-fastpath.sh
# is a comment, not an assertion. Replacing either `if` with `if false` (or
# deleting the branch outright) left this suite, test-pre-pr-lane.sh, and
# test-pre-pr-docs-fastpath.sh all green while step_test silently selected
# nothing for a canon-only CLAUDE.md/AGENTS.md diff -- the same failure mode
# #5935 shipped to close, reopened one call deeper.
#
# Each needle pins the WHOLE branch -- trigger condition, printf target, and
# the closing `fi` -- as one exact multiline block, matching the require_block
# style already used above for lane_input_paths. A presence check on a smaller
# fragment (e.g. just the trigger pattern, or just the target path alone) can
# be satisfied by a stray comment mentioning the same string -- the class of
# false-green a bare `rg -c` line count already has above (lane_gate_count is
# satisfiable by two comment lines matching that pattern). Pinning the full
# `if ... rg -q '<pattern>'; then / printf '<target>\n' / fi` shape closes that
# gap: no comment reproduces that exact three-line shell-syntax block, so the
# needle can only be satisfied by live code.
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require_block "golden snapshot fixture consumer mapping" '	if printf '"'"'%s\n'"'"' "${all}" | rg -q '"'"'^testdata/golden/'"'"'; then
		printf '"'"'./cmd/golden-corpus-gate\n'"'"'
	fi'
# shellcheck disable=SC2016
require_block "CLAUDE/AGENTS fixture consumer mapping" '	if printf '"'"'%s\n'"'"' "${all}" | rg -q '"'"'^(CLAUDE|AGENTS)\.md$'"'"'; then
		printf '"'"'./internal/runtime\n'"'"'
	fi'

printf 'PASS: pre-pr scheduling, worktree cache isolation, and lane wiring are pinned\n'
