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
# pre-commit hook feeds `filecap` every .go path in the repo (types: [go]), while
# `filecap-all` walks only `git ls-files 'go/*.go'` — so outside go/ the
# changed-files arm is stricter than CI on purpose, for the reasons in
# precommit-go.sh's file-cap header block. What each arm does with a LIST of
# inputs is covered separately by test_every_input_is_checked. Every scratch repo
# is built under mktemp -d with `env -u GIT_*` so the outer repo cannot leak in.
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

# new_repo sets REPO_DIR to a fresh scratch repo holding a copy of the helper (or
# of ${1}, used by the mutation check). It sets a global rather than printing: a
# command substitution would run it in a subshell and lose the scratch counter,
# silently reusing one repo for every case. The two workflow stubs exist because
# precommit-go.sh reads the pinned tool versions out of them at startup.
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

# run_gate runs one precommit-go.sh subcommand in a scratch repo and sets GATE_RC
# directly from the process (never through a pipe) plus GATE_OUT. Each run gets
# its OWN output path. A single shared path would be overwritten by the next run,
# so in assert_parity a `filecap` failure would dump `filecap-all`'s output and
# send the reader after the wrong arm.
#
# Set GATE_CWD to run from somewhere other than the repo root; run_gate clears it
# again so it cannot leak into the next case. Hence the absolute script path, and
# precommit-go.sh takes repo_root from `git rev-parse --show-toplevel`, so any
# directory inside the repo works.
run_gate() {
	local repo_dir="$1" sub="$2"
	shift 2
	local cwd="${GATE_CWD:-${repo_dir}}"
	GATE_CWD=""
	gate_seq=$((gate_seq + 1))
	GATE_OUT="${tmp_root}/gate-${gate_seq}-${sub}.out"
	set +e
	(
		cd "${cwd}" &&
			env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
				bash "${repo_dir}/scripts/dev/precommit-go.sh" "${sub}" "$@"
	) >"${GATE_OUT}" 2>&1
	GATE_RC=$?
	set -e
}

# assert_contains routes through pass() so it lands in the assertion total. When
# it did not, deleting one call — or stubbing out the gate's whole violation
# message and deleting all seven — left the run green at the pinned number,
# because the pin counted pass() calls only. That message is what this gate
# exists to deliver, so it gets counted guards.
assert_contains() {
	local needle="$1"
	rg -q --fixed-strings "${needle}" "${GATE_OUT}" ||
		fail "expected gate output to contain: ${needle}"
	pass "gate output contains: ${needle}"
}

# assert_equal, assert_rc, and assert_silent keep the comparison and the pass()
# inside ONE helper. That is the only shape the pinned total at the bottom can
# police: it notices a check that stopped running only when removing the check
# also removes a pass(). Exit-code and silence checks used to be written as bare
# `[[ ... ]] || fail` lines with a separate pass() beside them, so deleting the
# comparison left the total untouched — and two of them were the only thing
# standing between a real defect and a green run (the argv `status=0` reset in
# test_every_input_is_checked, and the `[[ -f ]]` guard in
# test_missing_file_is_ignored).
#
# What the pin does not police, before or after this: a check that still runs
# but was weakened in place. No suite detects its own assertions being relaxed
# by an editor. That is what the mutation runs in the PR body are for.
assert_equal() {
	local got="$1" want="$2" claim="$3"
	[[ "${got}" == "${want}" ]] || fail "${claim} — got '${got}', wanted '${want}'"
	pass "${claim}"
}

# assert_rc checks the exit code of the last run_gate.
assert_rc() {
	assert_equal "${GATE_RC}" "$1" "$2"
}

# assert_silent checks that the last run_gate printed nothing on either stream.
# A gate that reaches the right verdict while spraying rg or awk errors at the
# committer is still broken, and no exit code shows it.
assert_silent() {
	[[ ! -s "${GATE_OUT}" ]] || fail "$1 — the gate printed output"
	pass "$1"
}

# ---------------------------------------------------------------------------
# Parity matrix: for each input, `filecap` (changed-files) and `filecap-all`
# (whole-tree) must reach the SAME verdict, and it must be the expected one. One
# scratch repo per case, so filecap-all's whole-tree exit code is attributable to
# the single candidate file.
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
	# Point GATE_OUT at the arm that produced the unexpected verdict, not
	# whichever ran last, before the agreement check can fail.
	if [[ "${changed_rc}" == "${want_rc}" ]]; then
		GATE_OUT="${all_out}"
	else
		GATE_OUT="${changed_out}"
	fi
	assert_equal "${changed_rc}" "${all_rc}" \
		"parity ${label}: filecap and filecap-all agree on ${rel}"
	GATE_OUT="${changed_out}"
	assert_equal "${changed_rc}" "${want_rc}" \
		"parity ${label} (${rel}, ${lines} lines) -> rc=${want_rc} on both variants"
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
	# The negative half of that exemption, guarded on both sides of the colon,
	# because widening either side is its own one-token edit. Left of it:
	# filecap_check_file greps for the literal `nolint:filelength`; shortening
	# that to `nolint` — or to `lint` — exempts any file carrying ANY directive,
	# and 39 non-test .go files under go/ carry a different `nolint:` one today.
	# The longest, go/internal/collector/git_source_processing.go, is at 457
	# lines, so the widening hands it 43 lines of headroom nobody asked for.
	assert_parity "an unrelated nolint directive is NOT an exemption" \
		"go/internal/big/othermarked.go" 501 "nolint:gocyclo" 1
	# Right of it: dropping the `nolint:` prefix and grepping bare `filelength`
	# exempts any file that merely says the word — including the plugin's own
	# source, tools/golangci-lint-filelength/*.go. This fixture carries a real
	# directive for a DIFFERENT linter and names the plugin in prose.
	assert_parity "the word filelength without the nolint: prefix is NOT an exemption" \
		"go/internal/big/prosemention.go" 501 \
		"nolint:gocyclo // the filelength plugin is not involved" 1
	assert_parity "exactly 500 lines is legal" \
		"go/internal/big/boundary500.go" 500 "" 0
	assert_parity "501 lines is a violation" \
		"go/internal/big/boundary501.go" 501 "" 1
	# Prefix-only lookalikes must stay capped: each exemption is a path SEGMENT,
	# not a substring. That is what the plugin's "/generated/" (both separators)
	# buys and a bare `*generated*` glob loses. One per segment, because a widened
	# glob is a per-segment edit.
	assert_parity "generated_foo/ is NOT a generated/ segment" \
		"go/internal/generated_foo/oversize.go" 501 "" 1
	assert_parity "vendored/ is NOT a vendor/ segment" \
		"go/internal/vendored/oversize.go" 501 "" 1
	assert_parity "testdata_foo/ is NOT a testdata/ segment" \
		"go/internal/testdata_foo/oversize.go" 501 "" 1
	# The _test.go exemption is a SUFFIX in the plugin (strings.HasSuffix) and a
	# glob here, so relaxing the glob to `*_test*` exempts load_tester.go,
	# internal_testing.go, and anything else with `_test` mid-name. No other
	# fixture puts `_test` anywhere but the end.
	assert_parity "_test mid-name is not a _test.go suffix" \
		"go/internal/big/load_tester.go" 501 "" 1

	# An exact count, not a `> 0` floor: the floor cannot tell a dropped case
	# from a full matrix, and the exact count subsumes it. Read a mismatch as "a
	# case stopped running", not as a stale constant to bump.
	assert_equal "${parity_cases}" 14 "parity matrix evaluated all 14 cases"
}

# Iteration and accumulation. Every case above hands `filecap` one path and puts
# one .go file in the scratch repo, so nothing above notices an arm that stops
# after its first candidate, forgets an earlier violation, or stops REPORTING
# after the first one. Both production callers pass a list — the go-file-cap hook
# in .pre-commit-config.yaml forwards every staged .go path, pre-pr.sh's
# step_filecap forwards its own — and `filecap-all` walks every tracked
# `go/*.go` path (`git ls-files 'go/*.go' | awk 'END{print NR}'` said 12,392 the
# day this was written). Four fixtures, because the ordering that catches one
# defect hides another:
#
#   go/aaa/small.go      10 lines, clean, sorts FIRST in git ls-files order
#   go/zzz/oversize.go  501 lines, violation
#   go/zzz/oversize2.go 501 lines, violation
#   go/zzz/tail.go       10 lines, clean, sorts LAST in git ls-files order
#
# A violation last in argv catches a loop that stops early (`break`, or
# `for f in "${1:-}"`), and a violation first catches a `status=0` reset on each
# pass. A CLEAN file sorting last is what catches that reset on the WHOLE-TREE
# arm: with the only violation last in ls-files order, a reset still left status
# at 1 and filecap-all exited 1 for the wrong reason. That detector rests
# entirely on sort order, so the order is asserted below rather than assumed —
# renaming the fixture to go/zzz/aaa_tail.go silently hands the reset mutant a
# green run, and the name is a variable so the rename cannot miss the assertion.
# Two violations, both asserted by path, catch an arm that quits at the first one
# (`|| exit 1`, or a bare call under `set -e`) — rc stays 1 there, so no rc check
# can see it, and a committer would fix one file per commit cycle. go/zzz/ also
# sits outside go/internal/, catching a narrowed ls-files pattern, and `head -1`
# on that walk leaves only go/aaa.
#
# The limit of four fixtures, stated so nobody reads a green run as more than it
# is: a walk truncated at any N above 4 (`head -20`) looks identical to a
# complete walk here, and would look identical at any fixture count short of the
# real tree's 12,392. Building 13,000 scratch files to close that is not worth
# the runtime, so it stays a review-time check — the shape to reject is a bound
# on the walk at all, not a particular N.
test_every_input_is_checked() {
	local repo_dir tail_fixture="go/zzz/tail.go" last_tracked
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 10 "go/aaa/small.go"
	write_file "${repo_dir}" 501 "go/zzz/oversize.go"
	write_file "${repo_dir}" 501 "go/zzz/oversize2.go"
	write_file "${repo_dir}" 10 "${tail_fixture}"

	last_tracked="$(git_in "${repo_dir}" ls-files 'go/*.go' | tail -1)"
	assert_equal "${last_tracked}" "${tail_fixture}" \
		"the clean fixture sorts last in the whole-tree walk, which is what makes a status reset visible"

	run_gate "${repo_dir}" filecap "go/aaa/small.go" "go/zzz/oversize.go"
	assert_rc 1 "filecap checks every argument the hook stages, not just the first"
	assert_contains "go/zzz/oversize.go"

	run_gate "${repo_dir}" filecap "go/zzz/oversize.go" "go/aaa/small.go"
	assert_rc 1 "filecap remembers a violation once a clean file follows it"

	run_gate "${repo_dir}" filecap \
		"go/zzz/oversize.go" "go/aaa/small.go" "go/zzz/oversize2.go"
	assert_rc 1 "filecap keeps every violation in a mixed argv"
	assert_contains "go/zzz/oversize.go"
	assert_contains "go/zzz/oversize2.go"

	run_gate "${repo_dir}" filecap-all
	assert_rc 1 "filecap-all keeps a violation found before the last file"
	assert_contains "go/zzz/oversize.go"
	assert_contains "go/zzz/oversize2.go"
}

# Working directory, whole-tree arm. specs/ci-gates.v1.yaml registers
# `bash scripts/dev/precommit-go.sh filecap-all` as this gate's local.command and
# pins no directory to run it from. The walk is
# `git -C "${repo_root}" ls-files 'go/*.go'`; drop the `-C` and the pathspec
# resolves against the CALLER's directory, so from go/zzz/ it matches nothing,
# prints nothing, and exits 0 — while from the repo root the same broken arm
# looks perfect. Hence this case runs from a subdirectory; every other
# filecap-all assertion here runs from the root and cannot see it. For the
# changed-files arm the equivalent takes two cases, one per branch of
# filecap_check_file: test_violation_message for a path that violates, and
# test_nolint_marker_is_read_from_the_repo_root for one the marker exempts.
test_filecap_all_walks_from_the_repo_root() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "go/zzz/oversize.go"

	GATE_CWD="${repo_dir}/go/zzz"
	run_gate "${repo_dir}" filecap-all
	assert_rc 1 "filecap-all walks the repo root, not the caller's working directory"
	assert_contains "go/zzz/oversize.go"
}

# shellcheck source=lib/test-precommit-go-filecap-cases.sh
. "${repo_root}/scripts/lib/test-precommit-go-filecap-cases.sh"

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

	# These two are diagnostics, not the load-bearing checks, and they stay bare
	# guards for that reason. If the awk transform stops matching, the copy is
	# identical to the real script, `filecap` honours the skip, and changed_rc
	# comes back 0 — so the counted assertion below fails anyway. These just say
	# WHY, instead of leaving a reader to work it out from an rc of 0.
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

	assert_equal "${changed_rc}" 1 \
		"mutation check: the reverted filecap rejects the long _test.go"
	assert_equal "${all_rc}" 0 \
		"mutation check: filecap-all still passes the long _test.go"
	[[ "${changed_rc}" != "${all_rc}" ]] ||
		fail "mutation check: parity still held against the reverted fix — the matrix cannot detect the defect"
	pass "mutation check: reverting the fix breaks parity, so the matrix can detect the defect"
}

run_parity_matrix
test_every_input_is_checked
test_filecap_all_walks_from_the_repo_root
test_violation_message
test_nolint_marker_is_read_from_the_repo_root
test_nolint_must_be_a_directive_not_a_mention
test_package_clause_is_found_past_commented_decoys
test_non_go_file_is_ignored
test_outside_go_module_is_capped_locally_only
test_leading_segment_is_exempt
test_missing_file_is_ignored
test_missing_trailing_newline_counts
test_mutation_breaks_parity

# Pin the assertion total the way run_parity_matrix pins its case count. Deleting
# a call from the list above otherwise costs nothing: the runner still exits 0
# and still prints "all tests passed", with a smaller number nobody compares
# against anything. Read a mismatch as "a test stopped running", not as a stale
# constant to bump.
#
# What the pin covers: every check that goes through assert_contains,
# assert_equal, assert_rc, or assert_silent, because each of those calls pass()
# itself. Remove the call and the total drops. The exit-code and silence checks
# used to sit outside that set as bare `[[ ... ]] || fail` lines with their own
# pass() beside them, so deleting one changed nothing; two of them were the sole
# detector of a real defect.
#
# What the pin does NOT cover, and cannot: a check that still runs but was
# weakened in place, and the two bare diagnostic guards in
# test_mutation_breaks_parity (the `cmp -s` and `bash -n`), which are backed by
# the counted assertions after them.
[[ "${assertions}" == 70 ]] ||
	fail "runner made ${assertions} assertions, expected 70 — a test function or assertion went missing"

printf 'test-precommit-go-filecap: all tests passed (%d assertions, %d parity cases)\n' \
	"${assertions}" "${parity_cases}"
