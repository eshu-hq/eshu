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
#     -gate) — fast, and the test failures that matter live in what you touched.
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

base="origin/main"
git -C "${repo_root}" fetch --no-tags origin main >/dev/null 2>&1 || true
git -C "${repo_root}" rev-parse --verify "${base}" >/dev/null 2>&1 || base="HEAD~1"

# changed_go_files: committed (vs base) + staged + unstaged Go files under go/.
changed_go_files() {
	{
		git -C "${repo_root}" diff --name-only "${base}...HEAD"
		git -C "${repo_root}" diff --name-only HEAD
		git -C "${repo_root}" diff --name-only --cached
	} 2>/dev/null | sort -u | rg '^go/.*\.go$' || true
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

# changed_all_files: every changed path (committed vs base + staged + unstaged),
# not just Go files. Used to map non-Go fixtures to their consumer packages.
changed_all_files() {
	{
		git -C "${repo_root}" diff --name-only "${base}...HEAD"
		git -C "${repo_root}" diff --name-only HEAD
		git -C "${repo_root}" diff --name-only --cached
	} 2>/dev/null | sort -u
}

# fixture_consumer_dirs: ./-relative Go package dirs whose tests load a non-Go
# fixture that changed. pre-pr's focused `go test` is scoped to changed *Go*
# packages, so a fixture-only edit (e.g. the B-12 golden snapshot, which is a
# JSON file, not a Go package) would never run its consumer's tests locally — the
# exact gap that let a golden-snapshot change break go/cmd/golden-corpus-gate on
# CI only. Each entry maps a changed-path pattern to the package(s) that consume
# it; extend this table when a new fixture-backed test is added.
fixture_consumer_dirs() {
	local all
	all="$(changed_all_files)"
	# The B-12 golden snapshot is loaded by the golden-corpus-gate unit tests.
	if printf '%s\n' "${all}" | rg -q '^testdata/golden/'; then
		printf './cmd/golden-corpus-gate\n'
	fi
}

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
	while IFS= read -r d; do [[ -n "${d}" ]] && dirs+=("${d}"); done < <(changed_go_dirs)
	# Add packages whose tests load a changed non-Go fixture (deduped against the
	# changed-Go-package set), so a fixture-only edit still runs its consumer.
	while IFS= read -r d; do
		[[ -n "${d}" ]] || continue
		local seen=0 existing
		# Guard the array expansion: under `set -u`, "${dirs[@]}" on an empty array
		# is an unbound-variable error (the fixture-only-change case).
		if [[ ${#dirs[@]} -gt 0 ]]; then
			for existing in "${dirs[@]}"; do [[ "${existing}" == "${d}" ]] && seen=1 && break; done
		fi
		[[ ${seen} -eq 0 ]] && dirs+=("${d}")
	done < <(fixture_consumer_dirs)
	if [[ ${#dirs[@]} -eq 0 ]]; then
		printf 'no changed Go packages or fixtures vs %s — skipping focused tests\n' "${base}"
		return 0
	fi
	printf 'testing %d package(s) (changed Go packages + fixture consumers)\n' "${#dirs[@]}"
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

# step_exactness runs the credential-free exactness and telemetry contract gates
# that the changed paths select, via the shared gate registry (#4213/#4214). This
# replaces the old "remember the right verifier from the local-testing matrix"
# workflow: openapi/route-coverage/edge-source-tool/evidence-continuity/
# fact-kind/contract-source-of-truth/parser-relationship/query-plan/scale/
# capability-budget/collector-entrypoints/skill-roundtrip/telemetry-coverage/
# operator-dashboard etc. are now selected automatically. The --category filter
# keeps this to static contract gates: the race lane is #4215, and heavy
# pre-push gates (whole-module gosec, console e2e, frontend) stay out of pre-pr.
# Docker/NornicDB/Postgres/credentialed gates are CI-only and reported, not run.
step_exactness() {
	bash "${repo_root}/scripts/dev/run-selected-gates.sh" \
		--base "${base}" --tier pre-pr --category exactness,telemetry
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

run_whole_module_gates_parallel
run_step "go test (changed packages)" step_test
run_step "500-line file cap" step_filecap
run_step "package docs" step_docs
run_step "selected exactness + telemetry gates" step_exactness
run_step "race lane (Go changes)" step_race

printf '\n\033[1m==== pre-pr summary ====\033[0m\n'
for r in "${results[@]}"; do printf '%s\n' "${r}"; done
if [[ ${overall} -ne 0 ]]; then
	printf '\n\033[31mpre-pr: failures above — fix before pushing (CI runs the same gates).\033[0m\n'
else
	printf '\n\033[32mpre-pr: all local gates passed.\033[0m\n'
fi
exit ${overall}
