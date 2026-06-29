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
changed_go_dirs() {
	local f
	changed_go_files | while IFS= read -r f; do
		printf './%s\n' "$(dirname "${f#go/}")"
	done | sort -u
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

run_step "gofumpt (whole module)" step_fmt
run_step "golangci-lint (whole module)" step_lint
run_step "go build ./..." step_build
run_step "go vet ./..." step_vet
run_step "go test (changed packages)" step_test
run_step "500-line file cap" step_filecap
run_step "package docs" step_docs
run_step "selected exactness + telemetry gates" step_exactness

printf '\n\033[1m==== pre-pr summary ====\033[0m\n'
for r in "${results[@]}"; do printf '%s\n' "${r}"; done
if [[ ${overall} -ne 0 ]]; then
	printf '\n\033[31mpre-pr: failures above — fix before pushing (CI runs the same gates).\033[0m\n'
else
	printf '\n\033[32mpre-pr: all local gates passed.\033[0m\n'
fi
exit ${overall}
