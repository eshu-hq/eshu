#!/usr/bin/env bash
# pre-push: the fast local floor run once per PR, on the push that opens it, replacing the old
# per-SHA `make pre-pr` push stamp (removed — see
# docs/internal/agent-git-hygiene.md and docs/public/reference/local-testing.md).
#
#   bash scripts/dev/pre-push.sh        # or: make pre-push
#
# What it runs, no live Docker/NornicDB lane, no stamp:
#   (a) go test on fixture consumers plus any changed package with a
#       //go:build !race file (the same selection pre-pr.sh's step_test uses,
#       minus changed packages the race pass already covers: -race runs the
#       same tests, so a plain pass over them only repeats it; see
#       scripts/lib/pre-push-test-dedupe.sh), then go test -race on every
#       changed Go package (no cap; ESHU_PRE_PUSH_RACE_WARN_PACKAGES,
#       default 20, sets the package count above which it prints a warning);
#   (b) the 500-line Go file cap on changed files;
#   (c) gofumpt, golangci-lint, go build, and go vet scoped to changed Go
#       packages, for fast first feedback;
#   (c2) `go vet ./...` on the exact merge of the committed HEAD with the base
#       (origin/main unless ESHU_PRE_PUSH_BASE is set), built with
#       `git merge-tree` into a stable directory under this worktree's git
#       dir (scripts/lib/pre-push-merge.sh). A conflict fails closed. It is
#       skipped, with a message, when HEAD already contains the base or when
#       the merge's Go inputs equal the base's. It catches a head that compiles
#       alone but not on the main it will land on (#7053); it cannot catch a
#       combination that still compiles but behaves differently;
#   (d) the registry-selected blocking exactness/telemetry/hygiene/docs/security/frontend gates
#       for changed paths, at tier pre-pr, WITHOUT the whole-module prelude
#       (that prelude, and the whole-module go-build it adds, are `make pre-pr`'s
#       job). Whole-module go-fmt/go-lint/go-vet/go-file-cap and
#       telemetry-coverage are registered `local.pre_push: deferred` because
#       steps (b), (c), and (c2) already cover changed Go files and merge-tree
#       vet locally, while `test.yml/go-core` and `static-contract-gates.yml`
#       enforce the whole-module checks in CI. Also passes `--pre-push` to
#       run-selected-gates.sh, which makes the gate step an ALLOWLIST: only
#       gates registered `local.pre_push: floor` in specs/ci-gates.v1.yaml run
#       (fast gates chosen from local pre-pr/pre-push timing reports: directory
#       caps, package docs, perf-evidence, the contract registries, plus the
#       repo-wide sweep tests #7111 F5 added; and, since #8326, changed-package
#       gosec, measurement citations and the docs build, which only the git
#       pre-push hook ran before). The category list includes `security` and
#       `frontend` so blocking rows there print DEFER-CI instead of silently
#       never running. Every other triggered gate prints
#       `DEFER-CI <gate>: <reason>` and still runs in `make pre-pr` and CI.
#   (e) the advisory docs-contradiction gate, unconditionally. It has no CI
#       workflow at all (docs-contradiction is local-only by design), so
#       dropping the push stamp would otherwise remove its only enforcement.
#   (f) the ci-gate registry drift check (verify-ci-gates-registry.sh
#       --drift), unconditionally (#9615). Its gate's triggers are a fixed
#       list, and an added module a gate imports matches none of them, so a
#       trigger-gated run missed #9604's drift; about 1.5 s warm. It is not run
#       twice when the selected-gates step already ran ci-gate-registry.
#
# What it deliberately does NOT run: whole-module go-build, the whole-module
# race lane and the registry race gates, and
# the live Docker/NornicDB/Postgres lane (golden-corpus, replay-tier, security,
# frontend). Those stay `make pre-pr` / `make pre-pr-full`, which are not part
# of promotion: run `make pre-pr-full` for a package move (build tags can hide
# files from `./...`, so the whole-module race lane is what actually
# exercises them), `make pre-pr` to reproduce a CI red, or when the owner asks.
#
# The Ifá/Odù contract, performance, and end-to-end protection this floor does
# NOT reproduce lives in CI (test.yml's go-core-complete and go-race-complete,
# plus every Ifá/Odù and golden-corpus gate required-gates.yml aggregates into
# required-gates-complete) — see docs/public/reference/local-testing.md. A merge
# requires go-core-complete and go-race-complete green on the head and no
# completed red gate left undispositioned; the ruleset does not require
# required-gates-complete itself (#9089). This script is a fast local floor
# underneath that bar, not a replacement for it.
#
# Every step runs even if an earlier one fails (accumulate), so you see all
# problems at once. Exit status is non-zero if any step failed.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
go_dir="${repo_root}/go"
precommit="${repo_root}/scripts/dev/precommit-go.sh"
# #9066: cgo needs -std=gnu17 on GCC 15 + glibc 2.43; see scripts/lib/cgo-cflags.sh.
# shellcheck source=../lib/cgo-cflags.sh
source "${repo_root}/scripts/lib/cgo-cflags.sh"
eshu_export_cgo_cflags
# #9098: pin GOTOOLCHAIN to go/go.mod and clear GOROOT, so lint and gosec (and
# the gates that inherit this env) do not run under a newer host go. Child
# processes (precommit-go.sh, ci-gates) inherit it; see scripts/lib/go-toolchain.sh.
# shellcheck source=../lib/go-toolchain.sh
source "${repo_root}/scripts/lib/go-toolchain.sh"
eshu_export_go_toolchain "${repo_root}"
# shellcheck source=../lib/pre-pr-lane.sh
source "${repo_root}/scripts/lib/pre-pr-lane.sh"
# shellcheck source=../lib/pre-pr-fixture-consumers.sh
source "${repo_root}/scripts/lib/pre-pr-fixture-consumers.sh"
# shellcheck source=../lib/pre-pr-test-selection.sh
source "${repo_root}/scripts/lib/pre-pr-test-selection.sh"
# shellcheck source=../lib/pre-pr-go-paths.sh
source "${repo_root}/scripts/lib/pre-pr-go-paths.sh"
# shellcheck source=../lib/pre-push-merge.sh
source "${repo_root}/scripts/lib/pre-push-merge.sh"
# shellcheck source=../lib/gate-timing.sh
source "${repo_root}/scripts/lib/gate-timing.sh"
# shellcheck source=../lib/pre-push-test-dedupe.sh
source "${repo_root}/scripts/lib/pre-push-test-dedupe.sh"

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
# Measurement only (scripts/lib/gate-timing.sh): one JSONL line per run, never
# able to change a step's result. Summary: scripts/dev/gate-timing.py summary.
eshu_timing_init pre-push "${base}"

results=()
overall=0
run_step() {
	local name="$1"; shift
	local start=${SECONDS} rc=0
	printf '\n\033[1m==> %s\033[0m\n' "${name}"
	"$@" || rc=$?
	if [[ ${rc} -eq 0 ]]; then
		results+=("PASS  ${name} ($((SECONDS - start))s)")
	else
		results+=("FAIL  ${name} ($((SECONDS - start))s)")
		overall=1
	fi
	eshu_timing_note "${name}" "$((SECONDS - start))" "${rc}"
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

# step_test runs the plain `go test`. step_race below runs `go test -race` on
# every changed package, which runs the same tests, so a changed package is left
# out here unless a //go:build !race file means the race binary skips some of its
# tests (scripts/lib/pre-push-test-dedupe.sh). Fixture consumers and any other
# selected package the race run does not cover keep their plain run.
step_test() {
	local selected=() dirs=() d race_list="${pre_pr_state_dir}/race-dirs.txt"
	while IFS= read -r d; do
		[[ -n "${d}" ]] && selected+=("${d}")
	done < <({ changed_go_dirs; fixture_consumer_dirs; } | pre_pr_select_test_dirs)
	if [[ ${#selected[@]} -eq 0 ]]; then
		printf 'no changed Go packages or fixtures vs %s — skipping focused tests\n' "${base}"
		return 0
	fi
	changed_go_dirs >"${race_list}"
	while IFS= read -r d; do
		[[ -n "${d}" ]] && dirs+=("${d}")
	done < <(printf '%s\n' "${selected[@]}" | pre_push_plain_test_dirs "${race_list}")
	if [[ ${#dirs[@]} -eq 0 ]]; then
		printf 'plain go test skipped: the race run covers every one of the %d selected package target(s) (none has a //go:build !race file)\n' "${#selected[@]}"
		return 0
	fi
	printf 'testing %d of %d package target(s) with plain go test; changed packages without a //go:build !race file run only in the race step\n' "${#dirs[@]}" "${#selected[@]}"
	( cd "${go_dir}" && go test -count=1 "${dirs[@]}" ) 2>&1 | tee -a "${ESHU_TIMING_TEST_LOG:-/dev/null}"
	return "${PIPESTATUS[0]}"
}

# step_race runs `go test -race` on the changed Go packages only (#7111 F5).
# CI's go-race lane runs the whole module; this catches a race, or a test that
# only fails under the race detector's instrumentation (#7067's allocation
# count), in the packages the change touched before CI does. Measured warm:
# 8-25s for one package, 54s for go/internal/query on a cold cache. Fixture
# consumers get the plain `go test` above, not a race run.
step_race() {
	local dirs=() d
	while IFS= read -r d; do
		[[ -n "${d}" ]] && dirs+=("${d}")
	done < <(changed_go_dirs)
	if [[ ${#dirs[@]} -eq 0 ]]; then
		printf 'no changed Go packages vs %s — skipping the race run\n' "${base}"
		return 0
	fi
	printf 'race: %d changed package(s)\n' "${#dirs[@]}"
	# Single packages measured 8-54s and there is no cap, so a sweeping change
	# can run for many minutes (wall time for many packages is unmeasured). Warn
	# instead of skipping, since a silently skipped race run would read as
	# green. CI shards the whole module.
	if [[ ${#dirs[@]} -gt ${ESHU_PRE_PUSH_RACE_WARN_PACKAGES:-20} ]]; then
		printf 'race: %d changed package(s) exceeds ESHU_PRE_PUSH_RACE_WARN_PACKAGES=%s; single packages measured 8-54s, so this may run for minutes (CI shards the whole module).\n' \
			"${#dirs[@]}" "${ESHU_PRE_PUSH_RACE_WARN_PACKAGES:-20}"
	fi
	# -timeout is per test binary, the same 900s budget test.yml's go-race
	# shards use, so a hang fails in minutes instead of Go's default 10m.
	( cd "${go_dir}" && go test -race -count=1 -timeout 900s "${dirs[@]}" ) 2>&1 | tee -a "${ESHU_TIMING_RACE_LOG:-/dev/null}"
	return "${PIPESTATUS[0]}"
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
#
# `files` is filtered to paths that still exist on disk, the way changed_go_dirs
# (used for `dirs` below) already drops directories that no longer exist.
# Without the filter, a commit that deletes the last .go file in a package
# still hands that now-nonexistent path to `${precommit} fmt`/`lint`; those
# derive a package DIRECTORY from each file via precommit-go.sh's go_dirs
# (plain `dirname`, no existence check), and golangci-lint then fails the
# whole run with `lstat <dir>: no such file or directory` instead of simply
# having nothing left to format or lint in a deleted package.
step_fmt_lint_build_vet() {
	local files=() dirs=() f d status=0
	while IFS= read -r f; do
		[[ -n "${f}" ]] || continue
		[[ -f "${repo_root}/${f}" ]] || continue
		files+=("${f}")
	done < <(changed_go_files)
	while IFS= read -r d; do [[ -n "${d}" ]] && dirs+=("${d}"); done < <(changed_go_dirs)
	if [[ ${#files[@]} -eq 0 ]]; then
		printf 'no changed Go files still on disk — skipping fmt/lint/build/vet\n'
		return 0
	fi
	"${precommit}" fmt "${files[@]}" || status=1
	"${precommit}" lint "${files[@]}" || status=1
	if [[ ${#dirs[@]} -gt 0 ]]; then
		printf 'build+vet: %d changed package(s)\n' "${#dirs[@]}"
		# A single main package otherwise leaves an executable in go/, which
		# the source-identity gate correctly rejects as untracked input.
		# Multiple-package builds already discard output; /dev/null cannot
		# be used as one output file for multiple packages.
		if [[ ${#dirs[@]} -eq 1 ]]; then
			( cd "${go_dir}" && go build -o /dev/null "${dirs[@]}" ) || status=1
		else
			( cd "${go_dir}" && go build "${dirs[@]}" ) || status=1
		fi
		( cd "${go_dir}" && go vet "${dirs[@]}" ) || status=1
	fi
	return "${status}"
}

# The selected-gates output is kept so step_registry_drift can tell whether
# ci-gate-registry already ran there; ci-gates run prints "RUN      <id>:" for
# every gate it starts.
selected_gates_log=""
step_exactness() {
	selected_gates_log="${pre_pr_state_dir}/selected-gates.log"
	bash "${repo_root}/scripts/dev/run-selected-gates.sh" \
		--base "${base}" --tier pre-pr --category exactness,telemetry,hygiene,docs,security,frontend \
		--self-tests changed --blocking-only --pre-push \
		--report-file "${ESHU_TIMING_GATE_REPORT:-${pre_pr_state_dir}/timing-gates.json}" 2>&1 |
		tee "${selected_gates_log}"
	return "${PIPESTATUS[0]}"
}

# step_registry_drift runs the ci-gate registry drift check on every push,
# not only when one of ci-gate-registry's triggers matches (#9615). Its
# triggers are a fixed file list, and the drift it finds often sits in files
# outside that list: #9604 added a scripts/lib module that two gates' commands
# import, no trigger named it, so the check ran first in CI. Measured warm at
# 1.3-1.9 s (load average 26-28); the selected-gates step has already compiled
# ci-gates by the time this runs. When ci-gate-registry was selected and ran
# above, its result is already counted and this step does not run it again.
step_registry_drift() {
	if [[ -n "${selected_gates_log}" && -f "${selected_gates_log}" ]] &&
		rg -q '^RUN +ci-gate-registry:' "${selected_gates_log}"; then
		printf 'ci-gate-registry already ran in the selected-gates step, and its result is counted there; not running it twice\n'
		return 0
	fi
	bash "${repo_root}/scripts/verify-ci-gates-registry.sh" --drift
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
	eshu_timing_meta changed_go_packages "$(changed_go_dirs | wc -l | tr -d ' ')"
	run_step "go test (changed packages)" step_test
	run_step "go test -race (changed packages)" step_race
	run_step "500-line file cap" step_filecap
	run_step "gofumpt + lint + build + vet (changed packages)" step_fmt_lint_build_vet
	run_step "go vet on HEAD merged with ${base} (merge tree)" step_merge_vet
	run_step "selected local gates (exactness/telemetry/hygiene/docs/security/frontend)" step_exactness
	run_step "ci-gate registry drift (skipped if run above)" step_registry_drift
	run_step "docs-contradiction (advisory)" step_docs_contradiction
fi

eshu_timing_finish "${overall}"
printf '\n\033[1m==== pre-push summary ====\033[0m\n'
for r in "${results[@]}"; do printf '%s\n' "${r}"; done
if [[ -n "${pre_push_merge_summary}" ]]; then
	printf 'merge tree: %s\n' "${pre_push_merge_summary}"
fi
if [[ ${overall} -ne 0 ]]; then
	printf '\n\033[31mpre-push: failures above — fix before pushing.\033[0m\n'
else
	printf '\n\033[32mpre-push: all local gates passed.\033[0m\n'
	printf 'CI remains the blocking proof for Ifá/Odù contracts, performance, and end-to-end behavior: go-core-complete and go-race-complete must be green to merge, and no completed red gate may be left undispositioned.\n'
fi
exit ${overall}
