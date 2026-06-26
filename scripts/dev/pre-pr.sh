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
#   - go test: only the packages changed vs origin/main (fast; the test failures
#     that matter live in what you touched). Integration suites that need Postgres
#     or NornicDB are CI's job — see docs/public/reference/local-testing.md.
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
	if [[ ${#dirs[@]} -eq 0 ]]; then
		printf 'no changed Go packages vs %s — skipping focused tests\n' "${base}"
		return 0
	fi
	printf 'testing %d changed package(s)\n' "${#dirs[@]}"
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

run_step "gofumpt (whole module)" step_fmt
run_step "golangci-lint (whole module)" step_lint
run_step "go build ./..." step_build
run_step "go vet ./..." step_vet
run_step "go test (changed packages)" step_test
run_step "500-line file cap" step_filecap
run_step "package docs" step_docs

printf '\n\033[1m==== pre-pr summary ====\033[0m\n'
for r in "${results[@]}"; do printf '%s\n' "${r}"; done
if [[ ${overall} -ne 0 ]]; then
	printf '\n\033[31mpre-pr: failures above — fix before pushing (CI runs the same gates).\033[0m\n'
else
	printf '\n\033[32mpre-pr: all local gates passed.\033[0m\n'
fi
exit ${overall}
