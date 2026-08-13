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
# "Parity" here means one thing only: the same file gets the same verdict from
# both bash variants. It does NOT mean they see the same set of files. The
# pre-commit hook feeds `filecap` every .go path in the repo (types: [go]),
# while `filecap-all` walks only `git ls-files 'go/*.go'` — so outside go/ the
# changed-files arm is stricter than CI on purpose. precommit-go.sh's file-cap
# header block explains why. Nothing below asserts anything about input sets.
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
gate_seq=0

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
# ${1}. A non-empty ${4} is a lint directive written verbatim onto the package
# line — `nolint:filelength` for the exemption case, anything else for the
# negative case that proves an unrelated directive does not buy one.
write_file() {
	local repo_dir="$1" lines="$2" rel="$3" directive="${4:-}"
	local abs="${repo_dir}/${rel}" i=3
	mkdir -p "$(dirname "${abs}")"
	{
		printf '// SPDX-License-Identifier: MIT\n'
		if [[ -n "${directive}" ]]; then
			printf 'package fixture //%s // scratch fixture\n' "${directive}"
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
# Each run gets its OWN output path. A single shared path would be overwritten
# by the next run, so in assert_parity a `filecap` failure would dump
# `filecap-all`'s output and send the reader after the wrong arm.
run_gate() {
	local repo_dir="$1" sub="$2"
	shift 2
	gate_seq=$((gate_seq + 1))
	GATE_OUT="${tmp_root}/gate-${gate_seq}-${sub}.out"
	set +e
	(
		cd "${repo_dir}" &&
			env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
				bash scripts/dev/precommit-go.sh "${sub}" "$@"
	) >"${GATE_OUT}" 2>&1
	GATE_RC=$?
	set -e
}

# assert_contains routes through pass() so it lands in the assertion total. When
# it did not, deleting one call — or replacing the gate's whole violation message
# with a one-word stub and deleting all seven — left the run green at the pinned
# number, because the pin counted pass() calls only. The operator-facing message
# is the thing this gate exists to deliver, so it gets counted guards.
assert_contains() {
	local needle="$1"
	rg -q --fixed-strings "${needle}" "${GATE_OUT}" ||
		fail "expected gate output to contain: ${needle}"
	pass "gate output contains: ${needle}"
}

# ---------------------------------------------------------------------------
# Parity matrix: for each input, `filecap` (changed-files) and `filecap-all`
# (whole-tree) must reach the SAME verdict, and that verdict must be the
# expected one. One scratch repo per case so filecap-all's whole-tree exit code
# is attributable to the single candidate file.
# ---------------------------------------------------------------------------
assert_parity() {
	local label="$1" rel="$2" lines="$3" directive="$4" want_rc="$5"
	local repo_dir changed_rc all_rc changed_out all_out
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" "${lines}" "${rel}" "${directive}"

	run_gate "${repo_dir}" filecap "${rel}"
	changed_rc="${GATE_RC}"
	changed_out="${GATE_OUT}"
	run_gate "${repo_dir}" filecap-all
	all_rc="${GATE_RC}"
	all_out="${GATE_OUT}"

	parity_cases=$((parity_cases + 1))
	if [[ "${changed_rc}" != "${all_rc}" ]]; then
		# Dump the arm that produced the unexpected verdict, not whichever ran
		# last.
		if [[ "${changed_rc}" == "${want_rc}" ]]; then
			GATE_OUT="${all_out}"
		else
			GATE_OUT="${changed_out}"
		fi
		fail "${label}: parity broken — filecap rc=${changed_rc}, filecap-all rc=${all_rc} for ${rel}"
	fi
	if [[ "${changed_rc}" != "${want_rc}" ]]; then
		GATE_OUT="${changed_out}"
		fail "${label}: rc=${changed_rc}, wanted ${want_rc} for ${rel}"
	fi
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
		"go/internal/big/marked.go" 501 "nolint:filelength" 0
	# The negative half of that exemption. filecap_check_file greps for the
	# literal string `nolint:filelength`; widening that pattern to `nolint` — or
	# to `lint` — exempts any file carrying ANY directive, and 39 non-test .go
	# files under go/ carry a different `nolint:` one today. The longest,
	# go/internal/collector/git_source_processing.go, is at 457 lines, so the
	# widening would hand it 43 lines of headroom nobody asked for and the gate
	# would never say a word.
	assert_parity "an unrelated nolint directive is NOT an exemption" \
		"go/internal/big/othermarked.go" 501 "nolint:gocyclo" 1
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
	[[ "${parity_cases}" == 11 ]] ||
		fail "parity matrix evaluated ${parity_cases} cases, expected 11"
	pass "parity matrix evaluated ${parity_cases} cases"
}

# ---------------------------------------------------------------------------
# filecap-only cases: inputs filecap-all never sees, so there is no parity
# verdict to compare — only agreement with the CI plugin, or, for the outside-go/
# case below, a departure from it that precommit-go.sh documents as deliberate.
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

# First-party Go outside the go/ module. precommit-go.sh's header block calls
# this asymmetry deliberate: the hook stages every .go path in the repo, so
# `filecap` caps sdk/go and tools, while `filecap-all` and the CI plugin only
# ever look at go/. That makes the local arm the ONLY thing enforcing the repo's
# 500-line rule out here, which is exactly why it must not be quietly narrowed.
#
# Both verdicts are asserted because the claim is about the difference between
# them. Every other fixture in this file lives under go/, so a one-line
# `case "${f}" in go/*) ;; *) return 0 ;; esac` at the top of filecap_check_file
# — the precise narrowing the header block argues against — passed every
# assertion the harness had before this case existed.
test_outside_go_module_is_capped_locally_only() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "sdk/go/factschema/scratch.go"

	run_gate "${repo_dir}" filecap "sdk/go/factschema/scratch.go"
	[[ "${GATE_RC}" == 1 ]] ||
		fail "expected rc=1 for a 501-line first-party .go file outside go/, got ${GATE_RC}"
	assert_contains "sdk/go/factschema/scratch.go"
	assert_contains "501 lines"
	pass "a 501-line .go file outside go/ is capped by filecap"

	run_gate "${repo_dir}" filecap-all
	[[ "${GATE_RC}" == 0 ]] ||
		fail "expected rc=0 from filecap-all for a file outside its go/ walk, got ${GATE_RC}"
	pass "filecap-all does not see that file, so the strictness is local-only by design"
}

# The plugin matches "/testdata/" against an ABSOLUTE path, so a repo-root
# testdata/ tree is exempt in CI. The hook passes repo-RELATIVE paths, where
# that leading separator is absent; without the `<seg>/*` alternatives the local
# gate would reject a file CI never even lints. The split is latent, not live:
# no file in this repo exercises it today. The repo-root testdata/ tree holds
# two .go files, both _test.go and both under the cap, and there is no repo-root
# generated/ or vendor/ at all. These fixtures are the only thing holding each
# alternative in place.
#
# Each of the three gets its own written file and its own assertion. An earlier
# version asserted `generated/` with a path it never created, which meant
# filecap_check_file returned 0 at its `[[ -f ... ]]` guard before the skip
# logic ran: the assertion passed with or without the exemption, and the single
# pass line claimed all three segments were covered. Deleting a leading
# alternative from filecap_skip must turn this red, one alternative at a time.
test_leading_segment_is_exempt() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "testdata/nornicdb/oversize.go"
	write_file "${repo_dir}" 501 "generated/oversize.go"
	write_file "${repo_dir}" 501 "vendor/example.com/dep/oversize.go"

	run_gate "${repo_dir}" filecap "testdata/nornicdb/oversize.go"
	[[ "${GATE_RC}" == 0 ]] || fail "expected rc=0 for a 501-line repo-root testdata/ file, got ${GATE_RC}"
	pass "a leading testdata/ segment is exempt (501-line file on disk)"

	run_gate "${repo_dir}" filecap "generated/oversize.go"
	[[ "${GATE_RC}" == 0 ]] || fail "expected rc=0 for a 501-line repo-root generated/ file, got ${GATE_RC}"
	pass "a leading generated/ segment is exempt (501-line file on disk)"

	run_gate "${repo_dir}" filecap "vendor/example.com/dep/oversize.go"
	[[ "${GATE_RC}" == 0 ]] || fail "expected rc=0 for a 501-line repo-root vendor/ file, got ${GATE_RC}"
	pass "a leading vendor/ segment is exempt (501-line file on disk)"
}

# A path the hook stages but that does not exist on disk (deleted in the same
# commit, say) must not be a violation. This is what the old generated/ case was
# accidentally testing; keep it as its own explicit assertion so the guard is
# covered on purpose rather than by accident.
test_missing_file_is_ignored() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 10 "go/internal/big/present.go"
	run_gate "${repo_dir}" filecap "go/internal/big/absent.go"
	[[ "${GATE_RC}" == 0 ]] || fail "expected rc=0 for a path with no file on disk, got ${GATE_RC}"
	pass "a staged path with no file on disk is ignored"
	# The exit code alone does not hold the `[[ -f ]]` guard in place. Delete the
	# guard and rg and awk both run against a path that is not there: rg reports
	# an IO error, awk reports "can't open file", awk prints no count, and the
	# empty count still compares as 0 — so rc stays 0 and the assertion above
	# passes against the broken script. What breaks is the committer's terminal,
	# so that is what this asserts.
	[[ ! -s "${GATE_OUT}" ]] ||
		fail "expected no output for a path with no file on disk (rg/awk errors leaking?)"
	pass "and the gate stays silent, so no rg or awk error reaches the committer"
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
test_outside_go_module_is_capped_locally_only
test_leading_segment_is_exempt
test_missing_file_is_ignored
test_missing_trailing_newline_counts
test_mutation_breaks_parity

# Pin the assertion total the way run_parity_matrix pins its case count.
# Deleting a call from the list above otherwise costs nothing: the runner still
# exits 0 and still prints "all tests passed", just with a smaller number that
# nobody is comparing against anything. Every assertion increments the counter —
# assert_contains goes through pass() for exactly that reason — so this number
# also covers the message checks, not only the test functions. Update it when you
# add or remove an assertion, and read a mismatch as "a test stopped running",
# not as a stale constant to bump.
[[ "${assertions}" == 31 ]] ||
	fail "runner made ${assertions} assertions, expected 31 — a test function or assertion went missing"

printf 'test-precommit-go-filecap: all tests passed (%d assertions, %d parity cases)\n' \
	"${assertions}" "${parity_cases}"
