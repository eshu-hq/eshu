#!/usr/bin/env bash
# Self-test for the 500-line Go file cap in scripts/dev/precommit-go.sh.
#
# The cap lives in three places that must agree: the authoritative CI plugin
# (tools/golangci-lint-filelength/filelength.go, loaded by "Lint Go" in
# test.yml), the `filecap-all` whole-tree variant the ci-gates runner invokes,
# and the `filecap` changed-files variant the pre-commit hook and
# scripts/dev/pre-pr.sh invoke. `filecap` had none of the plugin's exemptions,
# so touching a long _test.go file was rejected locally while CI passed it
# (surfaced on PR #6104). The parity matrix below is the regression test for
# that; the mutation check at the end proves the matrix can actually fail.
#
# Every scratch repo is built under mktemp -d with `env -u GIT_*` so the outer
# repository cannot leak in.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/dev/precommit-go.sh"

if [[ ! -x "${helper}" ]]; then
	echo "test-precommit-go-filecap: missing executable helper at ${helper}" >&2
	exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

assertions=0
parity_cases=0
scratch_seq=0

fail() {
	echo "test-precommit-go-filecap: FAIL: $*" >&2
	if [[ -n "${GATE_OUT:-}" && -f "${GATE_OUT}" ]]; then
		echo "--- gate output ---" >&2
		sed -n '1,80p' "${GATE_OUT}" >&2
		echo "--- end gate output ---" >&2
	fi
	exit 1
}

pass() {
	assertions=$((assertions + 1))
	printf 'ok  %s\n' "$*"
}

git_in() {
	local repo_dir="$1"
	shift
	env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
		git -C "${repo_dir}" "$@"
}

# new_repo sets REPO_DIR to a fresh scratch repo holding a copy of the helper
# (or of ${1}, used by the mutation check). It sets a global rather than
# printing, because a command substitution would run it in a subshell and lose
# the scratch counter, silently reusing one repo for every case.
# The two workflow stubs exist because precommit-go.sh reads the pinned tool
# versions out of them at startup.
new_repo() {
	local script_src="${1:-${helper}}"
	scratch_seq=$((scratch_seq + 1))
	local repo_dir="${tmp_root}/repo-${scratch_seq}"
	mkdir -p "${repo_dir}/scripts/dev" "${repo_dir}/.github/workflows"
	cp "${script_src}" "${repo_dir}/scripts/dev/precommit-go.sh"
	chmod +x "${repo_dir}/scripts/dev/precommit-go.sh"
	printf 'run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2\n' \
		>"${repo_dir}/.github/workflows/test.yml"
	printf 'run: go install github.com/securego/gosec/v2/cmd/gosec@v2.27.1\n' \
		>"${repo_dir}/.github/workflows/security-scan.yml"
	git_in "${repo_dir}" init -q -b main
	git_in "${repo_dir}" config user.email "test@example.invalid"
	git_in "${repo_dir}" config user.name "Eshu Test"
	git_in "${repo_dir}" config core.hooksPath /dev/null
	REPO_DIR="${repo_dir}"
}

# write_file writes exactly ${2} lines to the repo-relative path ${3} under repo
# ${1}. A non-empty ${4} adds a //nolint:filelength marker on the package line.
write_file() {
	local repo_dir="$1" lines="$2" rel="$3" marker="${4:-}"
	local abs="${repo_dir}/${rel}" i=3
	mkdir -p "$(dirname "${abs}")"
	{
		printf '// SPDX-License-Identifier: MIT\n'
		if [[ -n "${marker}" ]]; then
			printf 'package fixture //nolint:filelength // scratch fixture\n'
		else
			printf 'package fixture\n'
		fi
		while ((i <= lines)); do
			printf '// filler line %d\n' "${i}"
			i=$((i + 1))
		done
	} >"${abs}"
	local actual
	actual="$(awk 'END { print NR }' "${abs}")"
	[[ "${actual}" == "${lines}" ]] || fail "fixture ${rel} has ${actual} lines, wanted ${lines}"
	git_in "${repo_dir}" add -A
	git_in "${repo_dir}" commit -q -m "fixture ${rel}"
}

# run_gate runs one precommit-go.sh subcommand in a scratch repo and sets
# GATE_RC directly from the process (never through a pipe) plus GATE_OUT.
run_gate() {
	local repo_dir="$1" sub="$2"
	shift 2
	GATE_OUT="${tmp_root}/gate.out"
	set +e
	(
		cd "${repo_dir}" &&
			env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
				bash scripts/dev/precommit-go.sh "${sub}" "$@"
	) >"${GATE_OUT}" 2>&1
	GATE_RC=$?
	set -e
}

assert_contains() {
	local needle="$1"
	rg -q --fixed-strings "${needle}" "${GATE_OUT}" ||
		fail "expected gate output to contain: ${needle}"
}

# ---------------------------------------------------------------------------
# Parity matrix: for each input, `filecap` (changed-files) and `filecap-all`
# (whole-tree) must reach the SAME verdict, and that verdict must be the
# expected one. One scratch repo per case so filecap-all's whole-tree exit code
# is attributable to the single candidate file.
# ---------------------------------------------------------------------------
assert_parity() {
	local label="$1" rel="$2" lines="$3" marker="$4" want_rc="$5"
	local repo_dir changed_rc all_rc
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" "${lines}" "${rel}" "${marker}"

	run_gate "${repo_dir}" filecap "${rel}"
	changed_rc="${GATE_RC}"
	run_gate "${repo_dir}" filecap-all
	all_rc="${GATE_RC}"

	parity_cases=$((parity_cases + 1))
	[[ "${changed_rc}" == "${all_rc}" ]] ||
		fail "${label}: parity broken — filecap rc=${changed_rc}, filecap-all rc=${all_rc} for ${rel}"
	[[ "${changed_rc}" == "${want_rc}" ]] ||
		fail "${label}: rc=${changed_rc}, wanted ${want_rc} for ${rel}"
	pass "parity ${label} (${rel}, ${lines} lines) -> rc=${want_rc} on both variants"
}

run_parity_matrix() {
	assert_parity "oversized non-test file is capped" \
		"go/internal/big/oversize.go" 501 "" 1
	assert_parity "oversized _test.go is exempt" \
		"go/internal/big/oversize_test.go" 501 "" 0
	assert_parity "generated/ segment is exempt" \
		"go/internal/generated/oversize.go" 501 "" 0
	assert_parity "vendor/ segment is exempt" \
		"go/vendor/example.com/dep/oversize.go" 501 "" 0
	assert_parity "testdata/ segment is exempt" \
		"go/internal/big/testdata/oversize.go" 501 "" 0
	assert_parity "//nolint:filelength marker is honoured" \
		"go/internal/big/marked.go" 501 marker 0
	assert_parity "exactly 500 lines is legal" \
		"go/internal/big/boundary500.go" 500 "" 0
	assert_parity "501 lines is a violation" \
		"go/internal/big/boundary501.go" 501 "" 1
	# Prefix-only lookalikes must stay capped: the exemption is a path SEGMENT,
	# not a substring. This is what the plugin's "/generated/" (with both
	# separators) buys, and what a bare `*generated*` glob would lose.
	assert_parity "generated_foo/ is NOT a generated/ segment" \
		"go/internal/generated_foo/oversize.go" 501 "" 1
	assert_parity "vendored/ is NOT a vendor/ segment" \
		"go/internal/vendored/oversize.go" 501 "" 1

	((parity_cases > 0)) ||
		fail "parity matrix evaluated 0 cases — the loop proved nothing"
	[[ "${parity_cases}" == 10 ]] ||
		fail "parity matrix evaluated ${parity_cases} cases, expected 10"
	pass "parity matrix evaluated ${parity_cases} cases"
}

# ---------------------------------------------------------------------------
# filecap-only cases: inputs filecap-all never sees, so there is no parity
# verdict to compare — only agreement with the CI plugin.
# ---------------------------------------------------------------------------
test_violation_message() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "go/internal/big/oversize.go"
	run_gate "${repo_dir}" filecap "go/internal/big/oversize.go"
	[[ "${GATE_RC}" == 1 ]] || fail "expected rc=1 for a 501-line non-test file, got ${GATE_RC}"
	assert_contains "go/internal/big/oversize.go"
	assert_contains "501 lines"
	assert_contains "split it"
	assert_contains "//nolint:filelength"
	pass "violation message names the file, the line count, and both legal exits"
}

test_non_go_file_is_ignored() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 900 "go/internal/big/notes.txt"
	run_gate "${repo_dir}" filecap "go/internal/big/notes.txt"
	[[ "${GATE_RC}" == 0 ]] || fail "expected rc=0 for a 900-line non-.go file, got ${GATE_RC}"
	pass "a non-.go file over the cap is ignored"
}

# The plugin matches "/testdata/" against an ABSOLUTE path, so a repo-root
# testdata/ tree is exempt in CI. The hook passes repo-RELATIVE paths, where
# that leading separator is absent; without the `testdata/*` alternative the
# local gate would reject a file CI never even lints. This repo has a
# repo-root testdata/ tree, so the case is reachable, not hypothetical.
test_leading_segment_is_exempt() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "testdata/nornicdb/oversize.go"
	run_gate "${repo_dir}" filecap "testdata/nornicdb/oversize.go"
	[[ "${GATE_RC}" == 0 ]] || fail "expected rc=0 for repo-root testdata/, got ${GATE_RC}"
	run_gate "${repo_dir}" filecap "generated/oversize.go"
	[[ "${GATE_RC}" == 0 ]] || fail "expected rc=0 for a missing repo-root generated/ path, got ${GATE_RC}"
	pass "a leading generated/, vendor/, or testdata/ segment is exempt"
}

# A file with no trailing newline: the plugin's bufio.Scanner counts the final
# partial line, `wc -l` does not. Counting with wc would let a 501-line file
# through locally and fail in CI.
test_missing_trailing_newline_counts() {
	local repo_dir abs
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 500 "go/internal/big/nonewline.go"
	abs="${repo_dir}/go/internal/big/nonewline.go"
	printf '// trailing partial line with no newline' >>"${abs}"
	git_in "${repo_dir}" add -A
	git_in "${repo_dir}" commit -q -m "drop trailing newline"
	run_gate "${repo_dir}" filecap "go/internal/big/nonewline.go"
	[[ "${GATE_RC}" == 1 ]] ||
		fail "expected rc=1 for 501 lines whose last line lacks a newline, got ${GATE_RC}"
	assert_contains "501 lines"
	pass "a final line without a trailing newline still counts (matches the plugin)"
}

# ---------------------------------------------------------------------------
# Mutation check: revert the fix inside a SCRATCH COPY of the script (never the
# real one) and prove the parity assertion goes red. A test that passes against
# both the fixed and the broken implementation is not a test.
# ---------------------------------------------------------------------------
test_mutation_breaks_parity() {
	local body="${tmp_root}/prefix-body.txt"
	local mutated="${tmp_root}/precommit-go-mutated.sh"
	local repo_dir changed_rc all_rc

	# The pre-fix `filecap` body, verbatim in shape: no skip() mirroring.
	printf '%s\n' \
		'			[[ "${f}" == *.go ]] || continue' \
		'			[[ -f "${repo_root}/${f}" ]] || continue' \
		'			rg -q "nolint:filelength" "${repo_root}/${f}" && continue' \
		'			lines="$(wc -l < "${repo_root}/${f}")"' \
		'			if (( lines > 500 )); then' \
		'				note "${f}: ${lines} lines exceeds the 500-line cap"' \
		'				status=1' \
		'			fi' \
		>"${body}"

	# The body is streamed in with getline rather than passed through `-v`:
	# BSD awk (the macOS default) rejects a literal newline in a -v assignment.
	awk -v bodyfile="${body}" '
		$0 == "\tfilecap)" { in_case = 1 }
		in_case && index($0, "filecap_check_file") {
			while ((getline line < bodyfile) > 0) { print line }
			close(bodyfile)
			next
		}
		in_case && $0 == "\t\t;;" { in_case = 0 }
		{ print }
	' "${helper}" >"${mutated}"
	chmod +x "${mutated}"

	if cmp -s "${helper}" "${mutated}"; then
		fail "mutation produced an identical script — the revert did not land, so this check proves nothing"
	fi
	bash -n "${mutated}" || fail "mutated script is not valid bash"
	pass "mutation landed (script differs and still parses)"

	new_repo "${mutated}"
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "go/internal/big/oversize_test.go"
	run_gate "${repo_dir}" filecap "go/internal/big/oversize_test.go"
	changed_rc="${GATE_RC}"
	run_gate "${repo_dir}" filecap-all
	all_rc="${GATE_RC}"

	[[ "${changed_rc}" == 1 ]] ||
		fail "mutation check: reverted filecap should reject the long _test.go, got rc=${changed_rc}"
	[[ "${all_rc}" == 0 ]] ||
		fail "mutation check: filecap-all should still pass the long _test.go, got rc=${all_rc}"
	[[ "${changed_rc}" != "${all_rc}" ]] ||
		fail "mutation check: parity still held against the reverted fix — the matrix cannot detect the defect"
	pass "mutation check: reverting the fix breaks parity (filecap rc=1 vs filecap-all rc=0), as required"
}

run_parity_matrix
test_violation_message
test_non_go_file_is_ignored
test_leading_segment_is_exempt
test_missing_trailing_newline_counts
test_mutation_breaks_parity

printf 'test-precommit-go-filecap: all tests passed (%d assertions, %d parity cases)\n' \
	"${assertions}" "${parity_cases}"
