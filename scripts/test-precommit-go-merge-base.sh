#!/usr/bin/env bash
# Self-test for the diff base that scripts/dev/precommit-go.sh hands to the
# measurement-citations gate (eshu-hq/eshu#7859).
#
# The wrapper used to pin the base to the TIP of origin/main. The gate's
# append-only ledger check compares the ledger at the base commit with the
# worktree ledger, so a branch that was merely BEHIND main by one new ledger row
# saw that row as "deleted" and a real push was refused. CI did not hit it: its
# HEAD is the pull request merge ref, which already holds main's rows. The
# wrapper must hand the gate the merge base.
#
# Fixture shape: a bare "origin", a work clone with a feature branch, and a
# second clone that lands a new ledger row on main AFTER the branch point. The
# wrapper runs on the feature branch and fetches that row itself, exactly as it
# does in a real push. Every repo is built under mktemp -d with `env -u GIT_*`
# so the outer repo cannot leak in.
#
# Cases:
#   1. behind main by a new ledger row, branch adds an innocuous file -> PASS
#   2. behind main, branch deletes an existing row                     -> FAIL
#   3. behind main, branch edits an existing row                       -> FAIL
#   4. behind main, branch adds an uncited measurement claim           -> FAIL
#   5. mutation: the wrapper pinned to the origin/main tip must FAIL case 1,
#      proving the test can see the defect it guards.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/dev/precommit-go.sh"
verifier="${repo_root}/scripts/verify-measurement-citations.sh"

for f in "${helper}" "${verifier}"; do
	if [[ ! -f "${f}" ]]; then
		echo "test-precommit-go-merge-base: missing ${f}" >&2
		exit 1
	fi
done

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

assertions=0
scratch_seq=0
GATE_RC=0
GATE_OUT=""

fail() {
	echo "test-precommit-go-merge-base: FAIL: $*" >&2
	if [[ -n "${GATE_OUT}" && -f "${GATE_OUT}" ]]; then
		echo "--- gate output ---" >&2
		sed -n '1,60p' "${GATE_OUT}" >&2
		echo "--- end gate output ---" >&2
	fi
	exit 1
}

pass() {
	assertions=$((assertions + 1))
	printf 'ok  %s\n' "$*"
}

git_in() {
	local dir="$1"
	shift
	env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
		git -C "${dir}" "$@"
}

row_a='{"id":"7859-row-a","date":"2026-10-09","issue":7859,"metric":"example_a","variant":"baseline","value":0,"unit":"trials","trials":10,"host":"local-dev","backend":"postgresql","backend_version":"16.14","commit":"deadbee0000","command":"go test ./a","note":"fixture row a"}'
row_b='{"id":"7859-row-b","date":"2026-10-09","issue":7859,"metric":"example_b","variant":"baseline","value":0,"unit":"trials","trials":10,"host":"local-dev","backend":"postgresql","backend_version":"16.14","commit":"deadbee0000","command":"go test ./b","note":"fixture row b"}'
row_main='{"id":"7859-row-main-only","date":"2026-10-09","issue":7859,"metric":"example_c","variant":"baseline","value":0,"unit":"trials","trials":10,"host":"local-dev","backend":"postgresql","backend_version":"16.14","commit":"deadbee0000","command":"go test ./c","note":"row that main gains after the branch point"}'

# new_fixture sets WORK_DIR to a feature-branch clone that is behind origin/main
# by one new ledger row (row_main). ${1} optionally names a helper copy to
# install in place of the real one (used by the mutation case).
new_fixture() {
	local helper_src="${1:-${helper}}"
	scratch_seq=$((scratch_seq + 1))
	local base_dir="${tmp_root}/fx-${scratch_seq}"
	local origin="${base_dir}/origin.git" work="${base_dir}/work" other="${base_dir}/other"
	mkdir -p "${base_dir}"
	env -u GIT_DIR -u GIT_WORK_TREE git init -q --bare -b main "${origin}"

	mkdir -p "${work}/scripts/dev" "${work}/.github/workflows" "${work}/docs/internal/evidence"
	git_in "${work}" init -q -b main
	git_in "${work}" config user.email "test@example.invalid"
	git_in "${work}" config user.name "Eshu Test"
	git_in "${work}" config core.hooksPath /dev/null
	# precommit-go.sh reads pinned tool versions out of these at startup.
	printf 'run: go-install-retry.sh github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2\n' \
		>"${work}/.github/workflows/test.yml"
	printf 'run: go-install-retry.sh github.com/securego/gosec/v2/cmd/gosec@v2.27.1\n' \
		>"${work}/.github/workflows/security-scan.yml"
	cp "${helper_src}" "${work}/scripts/dev/precommit-go.sh"
	cp "${verifier}" "${work}/scripts/verify-measurement-citations.sh"
	chmod +x "${work}/scripts/dev/precommit-go.sh" "${work}/scripts/verify-measurement-citations.sh"
	printf '%s\n%s\n' "${row_a}" "${row_b}" >"${work}/docs/internal/measurements.jsonl"
	printf '# Historical\n\nAn old finding (ledger:7859-row-a).\n' \
		>"${work}/docs/internal/evidence/historical.md"
	git_in "${work}" add -A
	git_in "${work}" commit -q -m "initial"
	git_in "${work}" remote add origin "${origin}"
	git_in "${work}" push -q origin main

	# The feature branch forks here, BEFORE main gains row_main.
	git_in "${work}" checkout -q -b feature

	env -u GIT_DIR -u GIT_WORK_TREE git clone -q "${origin}" "${other}"
	git_in "${other}" config user.email "test@example.invalid"
	git_in "${other}" config user.name "Eshu Test"
	git_in "${other}" config core.hooksPath /dev/null
	printf '%s\n' "${row_main}" >>"${other}/docs/internal/measurements.jsonl"
	git_in "${other}" add -A
	git_in "${other}" commit -q -m "main gains a ledger row"
	git_in "${other}" push -q origin main

	WORK_DIR="${work}"
}

feature_commit() {
	local dir="$1" msg="$2"
	git_in "${dir}" add -A
	git_in "${dir}" commit -q -m "${msg}"
}

# run_wrapper runs `precommit-go.sh measurement-citations` from the fixture and
# sets GATE_RC from the process itself (never through a pipe) plus GATE_OUT.
# GITHUB_BASE_REF and the verifier's own overrides are cleared so the wrapper's
# base choice is the only input under test.
run_wrapper() {
	local dir="$1" sub="${2:-measurement-citations}"
	GATE_OUT="${tmp_root}/gate-${scratch_seq}-${sub}.out"
	set +e
	(
		cd "${dir}" &&
			env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
				-u GITHUB_BASE_REF -u ESHU_MEASUREMENT_CITATIONS_BASE \
				-u ESHU_MEASUREMENT_CITATIONS_REPO_ROOT \
				bash scripts/dev/precommit-go.sh "${sub}"
	) >"${GATE_OUT}" 2>&1
	GATE_RC=$?
	set -e
}

expect_rc_zero() {
	[[ "${GATE_RC}" -eq 0 ]] || fail "$1: expected exit 0, got ${GATE_RC}"
	pass "$1"
}

expect_rc_nonzero() {
	[[ "${GATE_RC}" -ne 0 ]] || fail "$1: expected non-zero exit, got 0"
	pass "$1"
}

expect_out() {
	rg -q -F -- "$2" "${GATE_OUT}" || fail "$1: expected output to contain: $2"
	pass "$1"
}

expect_no_out() {
	if rg -q -F -- "$2" "${GATE_OUT}"; then
		fail "$1: output must not contain: $2"
	fi
	pass "$1"
}

# --- Case 1: behind main by a new row, branch adds an innocuous file. ---------
new_fixture
printf '# Note\n\nNothing measured here.\n' >"${WORK_DIR}/docs/internal/evidence/note.md"
feature_commit "${WORK_DIR}" "add a note"
run_wrapper "${WORK_DIR}"
expect_rc_zero "behind main by a new ledger row: gate passes"
expect_no_out "behind main: main's new row is not reported" "7859-row-main-only"

# --- Case 2: behind main, branch really deletes an existing row. -------------
new_fixture
printf '%s\n' "${row_b}" >"${WORK_DIR}/docs/internal/measurements.jsonl"
feature_commit "${WORK_DIR}" "delete row a"
run_wrapper "${WORK_DIR}"
expect_rc_nonzero "real row deletion on a behind-main branch: gate fails"
expect_out "deleted row is named" "row '7859-row-a' was deleted"
expect_no_out "main's new row is not blamed for the deletion" "7859-row-main-only"

# --- Case 3: behind main, branch really edits an existing row. ---------------
new_fixture
printf '%s\n%s\n' "${row_a}" "${row_b/fixture row b/rewritten note}" \
	>"${WORK_DIR}/docs/internal/measurements.jsonl"
feature_commit "${WORK_DIR}" "edit row b"
run_wrapper "${WORK_DIR}"
expect_rc_nonzero "real row edit on a behind-main branch: gate fails"
expect_out "edited row is named" "row '7859-row-b' was modified"
expect_no_out "main's new row is not blamed for the edit" "7859-row-main-only"

# --- Case 4: behind main, branch adds an uncited claim in an EARLIER commit. -
new_fixture
# The claim text is assembled with %s so this script's own source does not match the gate.
printf '# Finding\n\nA check ran clean: 0/30 %s failed.\n' trials \
	>"${WORK_DIR}/docs/internal/evidence/finding.md"
feature_commit "${WORK_DIR}" "add uncited claim"
printf '# Note\n\nNothing measured here.\n' >"${WORK_DIR}/docs/internal/evidence/note.md"
feature_commit "${WORK_DIR}" "innocuous tip commit"
run_wrapper "${WORK_DIR}"
expect_rc_nonzero "uncited claim in an earlier branch commit: gate fails"
expect_out "uncited claim is reported" "cites no ledger row"

# --- Case 5: mutation. The wrapper pinned to the origin/main tip must fail ---
# case 1, otherwise the cases above could not see the defect they guard.
mutated="${tmp_root}/precommit-go-tip-pin.sh"
# shellcheck disable=SC2016 # the pattern is a literal source line, not an expansion
sed 's|base="$(ledger_gate_base)"|base="origin/main"|' "${helper}" >"${mutated}"
if cmp -s "${helper}" "${mutated}"; then
	fail "mutation did not change the helper; the base line it targets moved"
fi
new_fixture "${mutated}"
printf '# Note\n\nNothing measured here.\n' >"${WORK_DIR}/docs/internal/evidence/note.md"
feature_commit "${WORK_DIR}" "add a note"
run_wrapper "${WORK_DIR}"
expect_rc_nonzero "mutation: tip-pinned wrapper is refused on a behind-main branch"
expect_out "mutation: tip pin reports main's row as deleted" "row '7859-row-main-only' was deleted"

printf 'test-precommit-go-merge-base: %d assertions passed\n' "${assertions}"
