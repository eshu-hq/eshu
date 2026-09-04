#!/usr/bin/env bash
# pre-pr: one-command local mirror of the CI "build" gate, so lint/build/test
# failures are caught in a single local pass instead of across multiple
# ~20-minute GitHub CI rounds. Run it before opening or updating a PR:
#
#   bash scripts/dev/pre-pr.sh        # or: make pre-pr
#
# Scope balances fidelity against speed:
#   - gofumpt + golangci-lint: WHOLE module (./...). The whole-module lint is the
#     point — it catches cross-package consequences a changed-package run misses,
#     e.g. code that becomes unused when a sibling package stops referencing it
#     (the exact class that accumulated as silent lint debt on main).
#   - go build / go vet: whole module.
#   - go test: the packages changed vs origin/main, PLUS any package whose tests
#     load a changed non-Go fixture (e.g. the B-12 golden snapshot → golden-corpus
#     -gate). A direct parent-parser change expands to ./internal/parser/... so
#     external child tests keep exercising Engine behavior, and a changed
#     package whose only black-box coverage sits in a sibling package also
#     selects that sibling (pre_pr_cross_package_test_dirs).
#     Integration suites that need Postgres or NornicDB are CI's job — see
#     docs/public/reference/local-testing.md.
#   - 500-line file cap + package docs: the cheap structural gates.
#
# The whole-module gates keep their full scope while reducing stacked wall
# time: go build and go vet run alongside the precommit helper lane, and that
# lane runs gofumpt before golangci-lint. fmt/lint stay serialized because both
# call scripts/dev/precommit-go.sh, whose result/config cache is worktree-local
# but remains single-writer within this preflight. On an 18-core/128GB dev box
# this keeps most of the speedup while avoiding first-run cache races.
#
# Every step runs even if an earlier one fails (accumulate), so you see all
# problems at once. Exit status is non-zero if any step failed.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
go_dir="${repo_root}/go"
precommit="${repo_root}/scripts/dev/precommit-go.sh"
# shellcheck source=../lib/pre-pr-docs-fastpath.sh
source "${repo_root}/scripts/lib/pre-pr-docs-fastpath.sh"
# shellcheck source=../lib/pre-pr-lane.sh
source "${repo_root}/scripts/lib/pre-pr-lane.sh"
# shellcheck source=../lib/pre-pr-fixture-consumers.sh
source "${repo_root}/scripts/lib/pre-pr-fixture-consumers.sh"
# shellcheck source=../lib/pre-pr-test-selection.sh
source "${repo_root}/scripts/lib/pre-pr-test-selection.sh"
# Root the classifier's path-existence check at the repo, so a deleted
# allowlisted file is recognized as deleted rather than as merely changed.
# shellcheck disable=SC2034  # read by the sourced classifier, not by this file.
PRE_PR_FASTPATH_ROOT="${repo_root}"

git -C "${repo_root}" fetch --no-tags origin main >/dev/null 2>&1 || true
pre_pr_resolve_lane_base "${repo_root}"
base="${PRE_PR_FASTPATH_BASE}"

# Cross-subshell state for this run; see pre_pr_git_state_init in
# scripts/lib/pre-pr-lane.sh for why the failure marker is a file and why the
# directory is probed rather than assumed.
pre_pr_git_state_init
# shellcheck disable=SC2154  # pre_pr_state_dir is set by the sourced library.
trap '[[ -n "${pre_pr_state_dir}" ]] && rm -rf "${pre_pr_state_dir}"' EXIT
pre_pr_gate_report="${pre_pr_state_dir}/selected-gates.json"

# changed_go_files: the Go files under go/ among those paths.
changed_go_files() {
	collect_changed_paths | sort -u | rg '^go/.*\.go$' || true
}

# changed_go_dirs: ./-relative package dirs (under go/) for the changed files.
# Directories that no longer exist on disk are dropped: a fully deleted package
# still appears in `git diff --name-only` (as removed files), so its dir would
# otherwise be handed to `go test`, which errors with "directory not found
# [setup failed]". CI's authoritative whole-module `go test ./...` skips absent
# dirs naturally; this focused selector must do the same.
changed_go_dirs() {
	local f d
	changed_go_files | while IFS= read -r f; do
		printf './%s\n' "$(dirname "${f#go/}")"
	done | sort -u | while IFS= read -r d; do
		[[ -d "${go_dir}/${d#./}" ]] && printf '%s\n' "${d}"
	done
}

# lane_input_paths: what the LANE decision looks at — everything above, plus
# untracked-but-not-ignored files. Only the lane reads them; every other gate
# mirrors what a push sends to CI, which an untracked file is not. The lane is
# the exception because the FULL lane's `go build ./...` compiles untracked
# files and the FAST lane skips that build. See the "Untracked files count"
# section of docs/public/reference/local-testing/pre-pr-docs-fastpath.md.
lane_input_paths() {
	{
		collect_changed_paths
		git_untracked_names
	} | sort -u
}

# collect_changed_paths, changed_all_files, and fixture_consumer_dirs now live
# in scripts/lib/pre-pr-fixture-consumers.sh (sourced above), so
# scripts/lib/test-pre-pr-fixture-consumers.sh can call fixture_consumer_dirs
# directly against a throwaway repository and assert what it actually emits,
# rather than text-matching its source in this file. The mapping covers the
# B-12 golden snapshot and the root CLAUDE.md/AGENTS.md canon files. Registry
# and workflow self-tests now run through ci-gate-registry's test_command
# instead of a duplicate package mapping (#5944).

# results accumulates one "PASS|FAIL  <name> (<n>s)" line per step.
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

step_fmt() { "${precommit}" fmt-all; }
step_lint() { "${precommit}" lint-all; }
step_build() { ( cd "${go_dir}" && go build ./... ); }
step_vet() { ( cd "${go_dir}" && go vet ./... ); }

# capture_whole_module_gate runs one whole-module gate, captures its output,
# and records duration at the point the gate exits. The parent prints results
# in a stable order after all lanes finish.
capture_whole_module_gate() {
	local tmpdir="$1" n="$2" label="$3"
	shift 3
	local start=${SECONDS} status=0
	{
		printf '\n\033[1m==> %s\033[0m\n' "${label}"
		if "$@"; then
			status=0
		else
			status=$?
		fi
	} >"${tmpdir}/${n}.log" 2>&1
	printf '%s\n' "${status}" >"${tmpdir}/${n}.status"
	printf '%s\n' "$((SECONDS - start))" >"${tmpdir}/${n}.duration"
	return 0
}

# run_precommit_gates_serial keeps this worktree's precommit-go cache
# single-writer.
# Build and vet still overlap with this lane, but fmt and lint do not overlap
# with each other.
run_precommit_gates_serial() {
	local tmpdir="$1"
	capture_whole_module_gate "${tmpdir}" fmt "gofumpt (whole module)" step_fmt
	capture_whole_module_gate "${tmpdir}" lint "golangci-lint (whole module)" step_lint
}

# run_whole_module_gates_parallel runs the race-free lanes concurrently:
# precommit helper checks (fmt then lint), go build, and go vet. Output remains
# per-step and printed in a fixed order, so a failure is never lost to
# interleaving.
run_whole_module_gates_parallel() {
	local names=(fmt lint build vet)
	local labels=("gofumpt (whole module)" "golangci-lint (whole module)" "go build ./..." "go vet ./...")
	local tmpdir pids=() i n status duration
	tmpdir="$(mktemp -d)"
	run_precommit_gates_serial "${tmpdir}" &
	pids+=($!)
	capture_whole_module_gate "${tmpdir}" build "go build ./..." step_build &
	pids+=($!)
	capture_whole_module_gate "${tmpdir}" vet "go vet ./..." step_vet &
	pids+=($!)

	for i in "${!pids[@]}"; do
		wait "${pids[$i]}" || true
	done
	for i in "${!names[@]}"; do
		n="${names[$i]}"
		status="$(cat "${tmpdir}/${n}.status" 2>/dev/null || printf "1")"
		duration="$(cat "${tmpdir}/${n}.duration" 2>/dev/null || printf "0")"
		if [[ "${status}" == "0" ]]; then
			results+=("PASS  ${labels[$i]} (${duration}s)")
		else
			results+=("FAIL  ${labels[$i]} (${duration}s)")
			overall=1
		fi
	done
	for i in "${!names[@]}"; do
		cat "${tmpdir}/${names[$i]}.log"
	done
	rm -rf "${tmpdir}"
}

step_test() {
	local dirs=() d
	# A direct parent-parser change selects the full parser tree so external
	# child-package tests exercise the parent Engine contract. Child-only changes
	# stay focused. The selector also deduplicates fixture consumers.
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

step_docs() {
	if changed_go_files | rg -q '^go/(internal|cmd)/'; then
		"${repo_root}/scripts/verify-package-docs.sh"
	else
		printf 'no go/internal|cmd changes — skipping package docs\n'
	fi
}

# step_tagged_builds compiles the Go files no other step in this gate looks at.
# A //go:build-gated file is excluded from the default tag set, so the whole
# lane above -- fmt, lint, build, vet, test -- walks straight past it, and a
# helper deleted elsewhere in its package leaves it uncompilable with nothing
# reporting it. That happened: #5167's live NornicDB complexity proof sat broken
# through a pre-pr run, a push, a full CI run and eight review rounds. The sweep
# is compile-only and needs no backend, so it belongs here rather than in the
# live lane below.
step_tagged_builds() {
	if changed_go_files | rg -q '^go/'; then
		"${repo_root}/scripts/verify-tagged-builds.sh" --all
	else
		printf 'no Go changes — skipping tagged-build sweep\n'
	fi
}

step_exactness() {
	local exactness_args=(
		--base "${base}" --tier pre-pr --category exactness,telemetry,hygiene,docs
		--self-tests changed --report-file "${pre_pr_gate_report}"
	)
	if [[ "${ESHU_PRE_PR_INCLUDE_ADVISORY:-0}" != "1" ]]; then
		exactness_args+=(--blocking-only)
	fi
	bash "${repo_root}/scripts/dev/run-selected-gates.sh" "${exactness_args[@]}" || return $?
	[[ -s "${pre_pr_gate_report}" ]] || {
		printf 'pre-pr: selected-gate runner returned success without its timing report\n' >&2
		return 1
	}
}

# step_race runs the local race lane for Go code changes (#4215). CI remains the
# authoritative blocking race gate (whole-module `go test ./... -race`); this is
# the fast local mirror that catches the common races before the PR waits on CI.
#   ESHU_PRE_PR_FULL_RACE=1 (make pre-pr-full): whole-module race, for high-risk PRs.
#   default: (1) the race-category registry gates the changed paths select
#     (targeted graph-write + replay race sets; reducer-contention is reported
#     CI-only, Postgres-backed); (2) scoped `-race` on changed Go packages that
#     NO locally-runnable race gate already covers — the exclusion is derived
#     from the registry (`ci-gates uncovered --category race`), not a hard-coded
#     list, so adding a new race gate cannot reintroduce a double-race or a gap.
step_race() {
	if [[ "${ESHU_PRE_PR_FULL_RACE:-0}" == "1" ]]; then
		printf 'full race: go test ./... -race (whole module)\n'
		( cd "${go_dir}" && go test ./... -race -count=1 -timeout 1200s )
		return
	fi
	local rc=0
	printf '== lane 1: race-category registry gates (targeted graph-write + replay; reducer-contention CI-only) ==\n'
	bash "${repo_root}/scripts/dev/run-selected-gates.sh" \
		--base "${base}" --tier pre-pr --category race || rc=1
	printf '== lane 2: scoped race for changed Go packages no race gate covers ==\n'
	local dirs=() seen=" " f rel
	while IFS= read -r f; do
		[[ -n "${f}" ]] || continue
		rel="./$(dirname "${f#go/}")"
		# Skip packages whose directory no longer exists (a fully deleted
		# race-uncovered package), for the same reason step_test filters them:
		# `go test -race ./deleted/pkg` fails with "directory not found".
		[[ -d "${go_dir}/${rel#./}" ]] || continue
		case "${seen}" in *" ${rel} "*) continue ;; esac
		seen="${seen}${rel} "
		dirs+=("${rel}")
	done < <(changed_go_files | ( cd "${go_dir}" && go run ./cmd/ci-gates uncovered \
		--registry "${repo_root}/specs/ci-gates.v1.yaml" --category race --tier pre-pr --paths-from - ) )
	if [[ ${#dirs[@]} -eq 0 ]]; then
		printf 'scoped race: no changed Go packages outside the registry race gates\n'
	else
		printf 'scoped race: %d package(s) not covered by a race gate\n' "${#dirs[@]}"
		( cd "${go_dir}" && go test -race -count=1 "${dirs[@]}" ) || rc=1
	fi
	printf 'note: CI runs the authoritative full `go test ./... -race`; `make pre-pr-full` runs it locally.\n'
	return ${rc}
}

# --- path-triggered live lane -------------------------------------------------
# The gates above are static and credential-free. The gates below block a PR in
# CI but need a live backend (Docker/NornicDB/Postgres) or a toolchain (Node,
# network). Each runs ONLY when the diff touches its trigger paths, and only if
# its prerequisite is present. A triggered gate whose prerequisite is missing is
# DEFERRED to CI with a loud warning and recorded in the pre-pr stamp, so a
# stamped push is honest about what it validated locally versus left to CI. This
# lane is what lets a green `make pre-pr` guarantee a green CI for the surfaces
# it can reach (golden-corpus is the common one). Force-defer everything with
# ESHU_PREPR_SKIP_LIVE=1 (records the deferral; CI stays the backstop for the
# deferred gates only).
#
# live_deferred only ever recorded ONE of the ways a stamped run can validate
# less than "everything": a triggered live gate whose prerequisite was
# missing, or a forced ESHU_PREPR_SKIP_LIVE=1. It said nothing about the
# documentation-only fast path (below, pre_pr_decide_lane) skipping the whole
# static Go lane and the race lane — those were only ever printed to the
# terminal's own summary (results+=("SKIP ...")), never persisted to the
# stamp file a later reader consults without having seen that terminal
# output. An empty stamp `deferred=` field was cited as "everything ran" on a
# run where the fast path had in fact skipped several steps — the field
# NAME, not just its emptiness, was the trap: it only ever tracked the live
# lane, so its being empty proved nothing about the fast-path lane. Recorded
# separately below as fast_path_skipped, and the stamp field itself renamed
# from the ambiguous "deferred=" to "live_lane_deferred=" so neither an empty
# value nor the bare key name can be misread as "nothing was skipped".
fast_path_skipped=()
live_deferred=()

# changed_paths was a byte-identical second copy of changed_all_files. Two
# copies of the same git plumbing meant a fix to one silently left the other
# swallowing exit codes, so there is now one implementation.
changed_paths() { changed_all_files; }

# run_or_defer <gate-name> <trigger-ERE> <prereq-cmd> <run-cmd...>
# Runs the gate when its paths changed and the prerequisite check passes; defers
# (records + warns) when triggered but the prerequisite is absent. Returns 1 only
# when a gate that actually RAN failed.
run_or_defer() {
	local name="$1" trigger="$2" prereq="$3"; shift 3
	# Materialize the path list first, then feed it to rg via a here-string. A
	# direct `changed_paths | rg -q` lets rg exit on its first match and SIGPIPE
	# the upstream git/sort; under `pipefail` that nonzero would hit `|| return 0`
	# and misclassify a genuinely-triggered gate as untriggered — skipping its
	# live proof while still stamping the commit. The here-string has no upstream
	# process to signal.
	local _changed; _changed="$(changed_paths)"
	rg -q "${trigger}" <<<"${_changed}" || return 0
	if [[ "${ESHU_PREPR_SKIP_LIVE:-0}" == "1" ]]; then
		printf '\033[33mlive lane: %s TRIGGERED but ESHU_PREPR_SKIP_LIVE=1 — DEFERRED to CI.\033[0m\n' "${name}"
		live_deferred+=("${name}"); return 0
	fi
	if ! eval "${prereq}" >/dev/null 2>&1; then
		printf '\033[33mlive lane: %s TRIGGERED but its prerequisite is unavailable — DEFERRED to CI. Provide it to validate locally.\033[0m\n' "${name}"
		live_deferred+=("${name}"); return 0
	fi
	printf '== live: %s ==\n' "${name}"
	"$@"
}

step_live() {
	local rc=0
	# Mirror .github/workflows/golden-corpus-gate.yml paths exactly, so any diff
	# that trips the corpus gate in CI also runs (or is honestly deferred) here —
	# not just the internal/ packages. Includes the go/cmd entrypoints, demospec,
	# the demo-first-answers spec, and the gate's own scripts/workflow.
	run_or_defer golden-corpus \
		'^(go/internal/(collector|parser|projector|reducer|query|relationships|storage|demospec|ask|askwiring|answerguardrail|answernarration)/|go/cmd/(bootstrap-index|ingester|projector|reducer|api|golden-corpus-gate|mock-prometheus-mimir|mock-openai-compatible)/|go/cmd/collector-|sdk/go/factschema/|testdata/(golden|cassettes)/|tests/fixtures/ecosystems/|specs/demo-first-answers\.v1\.yaml|scripts/(verify-golden-corpus-gate|test-verify-golden-corpus-gate)\.sh|scripts/lib/(golden-corpus-.+|test-golden-corpus-.+|live-gate-lock)\.sh|\.github/workflows/golden-corpus-gate\.yml)' \
		'docker info' \
		bash "${repo_root}/scripts/verify-golden-corpus-gate.sh" || rc=1
	run_or_defer replay-tier \
		'^(go/cmd/(ingester|projector)/|go/internal/(query|replay|reducer|storage/cypher|storage/nornicdb|projector|graph|runtime)/|testdata/cassettes/(replayoffline|replaydelta)/|scripts/(verify-replay-tier|test-verify-replay-tier)\.sh|scripts/dev/pre-pr\.sh|scripts/ci/install-apt-packages\.sh|\.github/workflows/verify-replay-tier\.yml)' \
		'docker info' \
		bash "${repo_root}/scripts/verify-replay-tier.sh" || rc=1
	# Go source only: a doc-adjacent edit under go/ (README.md, AGENTS.md) can't
	# carry a gosec/govulncheck finding, so it should not trigger the heavy local
	# security-preflight. (CI's security-scan has no path filter and remains the
	# backstop regardless.)
	run_or_defer security \
		'^(go/.*\.go|go\.mod|go\.sum)' \
		'true' \
		make -C "${repo_root}" security-preflight || rc=1
	run_or_defer frontend \
		'^(src/|apps/console/|package\.json|package-lock\.json)' \
		'command -v npm' \
		make -C "${repo_root}" frontend-preflight || rc=1
	if [[ ${#live_deferred[@]} -eq 0 ]]; then
		printf 'live lane: no path-triggered live gates were deferred.\n'
	fi
	return ${rc}
}

# --- documentation-only fast-path classification -----------------------------
# #5721: a docs/specs-only diff should never pay for the whole-module Go
# build/lint/vet/race lanes. The changed-package go test lane (step_test below)
# is never skipped wholesale -- it stays narrowly scoped to changed Go packages
# plus fixture_consumer_dirs on every run, fast or full, which is what still
# runs TestRepositoryDocumentationStandardsAreEnforced for a root
# AGENTS.md/CLAUDE.md-only diff (eshu-hq/eshu#5935 review). The allowlist
# classifier and the lane wiring live in scripts/lib/pre-pr-docs-fastpath.sh and
# scripts/lib/pre-pr-lane.sh; the rules and the failure classes they guard
# against are documented there and in
# docs/public/reference/local-testing/pre-pr-docs-fastpath.md.
#
# The decision itself is pre_pr_decide_lane, in the lane library, so a test can
# drive it: it runs the self-check, collects the paths, re-probes the state
# channel, reads the failure marker, and classifies -- in that order, because
# every other order can produce a FAST verdict on a run that failed to look.
#
# A failing self-check fails this run AND forces the FULL lane: a classifier
# that cannot pass its own table has not earned the right to skip anything.
pre_pr_decide_lane "${repo_root}" "${base}" "${PRE_PR_FASTPATH_BASE_STATUS}" lane_input_paths
if [[ "${PRE_PR_LANE_SELFCHECK_OK}" == "1" ]]; then
	results+=("PASS  docs fast-path classifier self-check (${PRE_PR_LANE_SELFCHECK_SECONDS}s)")
else
	results+=("FAIL  docs fast-path classifier self-check (${PRE_PR_LANE_SELFCHECK_SECONDS}s)")
	overall=1
fi

if [[ ${#PRE_PR_LANE_CHANGED_PATHS[@]} -gt 0 ]]; then
	pre_pr_print_lane_banner "${base}" "${PRE_PR_LANE_CHANGED_PATHS[@]}"
else
	pre_pr_print_lane_banner "${base}"
fi

# Both lane gates ask whether the lane is NOT "fast", never whether it is
# "full", and pre_pr_print_lane_banner asks the same way. A third value cannot
# be produced today, but if one ever were, `== "full"` here would skip the Go
# lanes while the banner said FULL and the run still stamped the SHA -- the
# invisible-failure shape this whole fast path exists to prevent.
if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then
	run_whole_module_gates_parallel
else
	while IFS= read -r pre_pr_skip_name; do
		results+=("SKIP  ${pre_pr_skip_name} (documentation-only fast path)")
		fast_path_skipped+=("${pre_pr_skip_name}")
	done < <(pre_pr_fast_lane_skip_steps)
fi
# go test runs on BOTH lanes, always. Its own scope (changed-Go-package dirs
# plus fixture_consumer_dirs) already narrows to nothing on a genuinely
# docs-only diff -- see the file-cap and package-docs steps below for the same
# always-run-but-no-op pattern. Skipping this step wholesale on the FAST lane,
# as pre-pr.sh used to do, made fixture_consumer_dirs's CLAUDE.md/AGENTS.md
# mapping dead code for the one diff shape it exists to catch: a root-agent-
# file-only change both qualifies for FAST and needs that guard to run
# (eshu-hq/eshu#5935 review). See pre_pr_fast_lane_skip_steps in
# scripts/lib/pre-pr-lane.sh for the regression guard.
run_step "go test (changed packages)" step_test
run_step "500-line file cap" step_filecap
run_step "package docs" step_docs
run_step "tagged build sweep" step_tagged_builds
run_step "selected exactness + telemetry gates" step_exactness
if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then
	run_step "race lane (Go changes)" step_race
else
	results+=("SKIP  race lane (Go changes) (documentation-only fast path)")
	fast_path_skipped+=("race lane (Go changes)")
fi
run_step "path-triggered live lane" step_live

printf '\n\033[1m==== pre-pr summary ====\033[0m\n'
for r in "${results[@]}"; do printf '%s\n' "${r}"; done
if [[ ${overall} -ne 0 ]]; then
	printf '\n\033[31mpre-pr: failures above — fix before pushing (CI runs the same gates).\033[0m\n'
else
	# Stamp this exact HEAD for the pre-push hook and retain the command-level
	# timing report beside it. Both records are keyed by SHA, so rebases and
	# amends invalidate them without cross-worktree collisions.
	head_sha="$(git -C "${repo_root}" rev-parse HEAD 2>/dev/null || true)"
	common_dir="$(git -C "${repo_root}" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
	if [[ -n "${head_sha}" && -n "${common_dir}" ]]; then
		stamp_dir="${common_dir}/eshu-prepr-stamp"
		mkdir -p "${stamp_dir}"
		install -m 0600 "${pre_pr_gate_report}" "${stamp_dir}/${head_sha}.gates.json" || exit 1
		printf 'sha=%s\nlive_lane_deferred=%s\nfast_path_skipped=%s\n' \
			"${head_sha}" "${live_deferred[*]:-}" "${fast_path_skipped[*]:-}" > "${stamp_dir}/${head_sha}"
		printf 'gate_report=%s.gates.json\n' "${head_sha}" >> "${stamp_dir}/${head_sha}"
		printf '\n\033[32mpre-pr: all local gates passed — stamped %s' "${head_sha:0:12}"
		[[ ${#live_deferred[@]} -gt 0 ]] && printf ' (deferred to CI: %s)' "${live_deferred[*]}"
		[[ ${#fast_path_skipped[@]} -gt 0 ]] && printf ' (fast-path skipped: %s)' "${fast_path_skipped[*]}"
		printf '.\033[0m\n'
	else
		printf '\n\033[32mpre-pr: all local gates passed.\033[0m\n'
	fi
fi
exit ${overall}
