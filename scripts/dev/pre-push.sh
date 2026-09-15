#!/usr/bin/env bash
# pre-push: the fast local floor run before every push, replacing the old
# per-SHA `make pre-pr` push stamp (removed — see
# docs/internal/agent-git-hygiene.md and docs/public/reference/local-testing.md).
#
#   bash scripts/dev/pre-push.sh        # or: make pre-push
#
# What it runs, no race lane, no live Docker/NornicDB lane, no stamp:
#   (a) go test on changed Go packages plus fixture consumers (the same
#       selection pre-pr.sh's step_test uses);
#   (b) the 500-line Go file cap on changed files;
#   (c) gofumpt, golangci-lint, go build, and go vet scoped to changed Go
#       packages, for fast first feedback;
#   (d) the registry-selected blocking exactness/telemetry/hygiene/docs gates
#       for changed paths, at tier pre-pr, WITHOUT the whole-module prelude
#       (that prelude, and the whole-module go-build it adds, are `make pre-pr`'s
#       job). Note this DOES include go-fmt/go-lint/go-vet whenever go/**
#       changed: those three registry rows are whole-module by definition (no
#       changed-package variant exists in the registry), so they still run
#       whole-module here too, on top of (c)'s faster scoped pass — the
#       cross-package coverage pre-pr.sh's own header describes is real value,
#       not accidental duplication. Also passes `--pre-push` to
#       run-selected-gates.sh, which makes the gate step an ALLOWLIST: only
#       gates registered `local.pre_push: floor` in specs/ci-gates.v1.yaml run
#       (24 fast gates chosen from 67 local pre-pr timing reports: lint, file
#       and directory caps, package docs, perf-evidence, telemetry coverage,
#       the contract registries). Every other triggered gate prints
#       `DEFER-CI <gate>: <reason>` and still runs in `make pre-pr` and CI.
#       A denylist of the slowest gates was tried first and still took more
#       than 15 minutes on a one-line Go change; the allowlist took 400s;
#   (e) the advisory docs-contradiction gate, unconditionally. It has no CI
#       workflow at all (docs-contradiction is local-only by design), so
#       dropping the push stamp would otherwise remove its only enforcement.
#
# What it deliberately does NOT run: whole-module go-build, the race lane, and
# the live Docker/NornicDB/Postgres lane (golden-corpus, replay-tier, security,
# frontend). Those stay `make pre-pr` / `make pre-pr-full` — recommended, not
# required, before pushing a change to queue/lease/claim code, schema DDL,
# hot-path Cypher or graph writes, reducer projection/materialization, or a
# package move (for moves, prefer `make pre-pr-full`: build tags can hide
# files from `./...`, so the whole-module race lane is what actually
# exercises them).
#
# The Ifá/Odù contract, performance, and end-to-end protection this floor does
# NOT reproduce lives in CI's required-gates-complete aggregate (test.yml's
# go-core-complete and go-race-complete plus every Ifá/Odù and golden-corpus
# gate required-gates.yml aggregates) — see
# docs/public/reference/local-testing.md. A merge still requires
# required-gates-complete green; this script is a fast local floor underneath
# that authority, not a replacement for it.
#
# Every step runs even if an earlier one fails (accumulate), so you see all
# problems at once. Exit status is non-zero if any step failed.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
go_dir="${repo_root}/go"
precommit="${repo_root}/scripts/dev/precommit-go.sh"
# shellcheck source=../lib/pre-pr-lane.sh
source "${repo_root}/scripts/lib/pre-pr-lane.sh"
# shellcheck source=../lib/pre-pr-fixture-consumers.sh
source "${repo_root}/scripts/lib/pre-pr-fixture-consumers.sh"
# shellcheck source=../lib/pre-pr-test-selection.sh
source "${repo_root}/scripts/lib/pre-pr-test-selection.sh"
# shellcheck source=../lib/pre-pr-go-paths.sh
source "${repo_root}/scripts/lib/pre-pr-go-paths.sh"

git -C "${repo_root}" fetch --no-tags origin main >/dev/null 2>&1 || true
# ESHU_PRE_PUSH_BASE compares against another ref, for a branch stacked on an
# unmerged branch. Default: origin/main. An unresolvable base fails the run:
# silently narrowing to HEAD~1 would check only the last commit's paths and
# still print "all local gates passed".
base="${ESHU_PRE_PUSH_BASE:-origin/main}"
if ! git -C "${repo_root}" rev-parse --verify "${base}^{commit}" >/dev/null 2>&1; then
	printf 'pre-push: base %s does not resolve to a commit; set ESHU_PRE_PUSH_BASE to a valid ref (or fetch origin main) and rerun.\n' "${base}" >&2
	exit 2
fi

# Cross-subshell state so a failed git collector cannot silently read as
# "nothing changed" — see pre_pr_git_state_init's own doc comment in
# scripts/lib/pre-pr-lane.sh for why this has to be a file, not a variable.
pre_pr_git_state_init
# shellcheck disable=SC2154  # pre_pr_state_dir is set by the sourced library.
trap '[[ -n "${pre_pr_state_dir}" ]] && rm -rf "${pre_pr_state_dir}"' EXIT

results=()
overall=0
run_step() {
	local name="$1"; shift
	local start=${SECONDS}
	printf '\n\033[1m==> %s\033[0m\n' "${name}"
	if "$@"; then
		results+=("PASS  ${name} ($((SECONDS - start))s)")
	else
		results+=("FAIL  ${name} ($((SECONDS - start))s)")
		overall=1
	fi
}

# A failed changed-path collector must not silently look like "nothing
# changed" — that would skip every step below on a run that never looked.
# Sets pre_push_diff_broken_reason (non-empty iff the state channel or a git
# collector failed) so the caller checks the channel exactly once.
pre_push_diff_broken_reason=""
check_diff_state() {
	pre_pr_git_state_check
	if [[ -n "${pre_pr_state_broken}" ]]; then
		pre_push_diff_broken_reason="${pre_pr_state_broken}"
	elif [[ -n "${pre_pr_diff_fail_marker}" && -e "${pre_pr_diff_fail_marker}" ]]; then
		pre_push_diff_broken_reason="a git diff against ${base} failed; the changed-path list is incomplete"
	fi
}

step_test() {
	local dirs=() d
	while IFS= read -r d; do
		[[ -n "${d}" ]] && dirs+=("${d}")
	done < <({ changed_go_dirs; fixture_consumer_dirs; } | pre_pr_select_test_dirs)
	if [[ ${#dirs[@]} -eq 0 ]]; then
		printf 'no changed Go packages or fixtures vs %s — skipping focused tests\n' "${base}"
		return 0
	fi
	printf 'testing %d package target(s) (changed Go packages + fixture consumers)\n' "${#dirs[@]}"
	( cd "${go_dir}" && go test -count=1 "${dirs[@]}" )
}

step_filecap() {
	local files=() f
	while IFS= read -r f; do [[ -n "${f}" ]] && files+=("${f}"); done < <(changed_go_files)
	if [[ ${#files[@]} -eq 0 ]]; then
		printf 'no changed Go files — skipping file cap\n'
		return 0
	fi
	"${precommit}" filecap "${files[@]}"
}

# step_fmt_lint_build_vet: gofumpt + golangci-lint (precommit-go.sh's existing
# changed-package modes) plus go build/go vet scoped to the same changed
# package dirs (precommit-go.sh has no build/vet mode at all — whole-module or
# otherwise — so those two run directly here rather than growing a script
# already past the repo's own line cap).
step_fmt_lint_build_vet() {
	local files=() dirs=() f d status=0
	while IFS= read -r f; do [[ -n "${f}" ]] && files+=("${f}"); done < <(changed_go_files)
	while IFS= read -r d; do [[ -n "${d}" ]] && dirs+=("${d}"); done < <(changed_go_dirs)
	if [[ ${#files[@]} -eq 0 ]]; then
		printf 'no changed Go files — skipping fmt/lint/build/vet\n'
		return 0
	fi
	"${precommit}" fmt "${files[@]}" || status=1
	"${precommit}" lint "${files[@]}" || status=1
	if [[ ${#dirs[@]} -gt 0 ]]; then
		printf 'build+vet: %d changed package(s)\n' "${#dirs[@]}"
		( cd "${go_dir}" && go build "${dirs[@]}" ) || status=1
		( cd "${go_dir}" && go vet "${dirs[@]}" ) || status=1
	fi
	return "${status}"
}

step_exactness() {
	bash "${repo_root}/scripts/dev/run-selected-gates.sh" \
		--base "${base}" --tier pre-pr --category exactness,telemetry,hygiene,docs \
		--self-tests changed --blocking-only --pre-push
}

# step_docs_contradiction runs the advisory docs-contradiction gate
# unconditionally rather than trigger-gated like the registry rows above. It
# has no CI counterpart (specs/ci-gates.v1.yaml's docs-contradiction entry is
# local-only by design), so this floor is the ONLY place it ever runs; scanning
# every docs/public/**/*.md page is one cheap awk pass, and the gate itself
# stays advisory (it only fails the run when DOCS_CONTRADICTION_ENFORCE=true).
step_docs_contradiction() {
	bash "${repo_root}/scripts/verify-docs-contradiction.sh"
}

# Collect once upfront, purely to populate the failure marker before any step
# below decides "nothing changed" from the same collectors.
changed_all_files >/dev/null
check_diff_state

if [[ -n "${pre_push_diff_broken_reason}" ]]; then
	printf '\npre-push: %s — cannot trust the changed-path list, failing closed.\n' "${pre_push_diff_broken_reason}" >&2
	results+=("FAIL  changed-path collection (${pre_push_diff_broken_reason})")
	overall=1
else
	run_step "go test (changed packages)" step_test
	run_step "500-line file cap" step_filecap
	run_step "gofumpt + lint + build + vet (changed packages)" step_fmt_lint_build_vet
	run_step "selected local gates (exactness/telemetry/hygiene/docs)" step_exactness
	run_step "docs-contradiction (advisory)" step_docs_contradiction
fi

printf '\n\033[1m==== pre-push summary ====\033[0m\n'
for r in "${results[@]}"; do printf '%s\n' "${r}"; done
if [[ ${overall} -ne 0 ]]; then
	printf '\n\033[31mpre-push: failures above — fix before pushing.\033[0m\n'
else
	printf '\n\033[32mpre-push: all local gates passed.\033[0m\n'
	printf 'CI required-gates remain the blocking proof for Ifá/Odù contracts, performance, and end-to-end behavior (required-gates-complete must still be green to merge).\n'
fi
exit ${overall}
