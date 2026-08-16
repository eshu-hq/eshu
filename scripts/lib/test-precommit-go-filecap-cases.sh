#!/usr/bin/env bash
# filecap-only cases for scripts/test-precommit-go-filecap.sh: inputs that the
# whole-tree `filecap-all` arm never sees, so there is no parity verdict to
# take -- only agreement with the CI plugin, or, for the outside-go/ case, a
# departure precommit-go.sh documents as deliberate.
#
# Sourced by that driver; not runnable standalone. Relies on the harness it
# already defines (new_repo, write_file, run_gate, assert_contains, assert_rc,
# assert_silent).
# ---------------------------------------------------------------------------
# filecap-only cases: inputs filecap-all never sees, so there is no parity
# verdict — only agreement with the CI plugin, or, for the outside-go/ case
# below, a departure precommit-go.sh documents as deliberate.
# ---------------------------------------------------------------------------
test_violation_message() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "go/internal/big/oversize.go"
	run_gate "${repo_dir}" filecap "go/internal/big/oversize.go"
	assert_rc 1 "a 501-line non-test file is a violation"
	assert_contains "go/internal/big/oversize.go"
	assert_contains "501 lines"
	assert_contains "split it"
	assert_contains "//nolint:filelength"

	# Same file, same argv, run from go/ instead of the repo root. The hook stages
	# repo-relative paths and filecap_check_file resolves them against repo_root
	# for that reason; drop the `${repo_root}/` prefix and the `[[ -f ]]` guard
	# misses, so every file passes. Both callers run from the root today.
	GATE_CWD="${repo_dir}/go"
	run_gate "${repo_dir}" filecap "go/internal/big/oversize.go"
	assert_rc 1 "a violating file is still a violation when the gate runs from a subdirectory"
	assert_contains "501 lines"
}

# The other half of that working-directory contract, and the half nothing used to
# cover. filecap_check_file resolves THREE paths against ${repo_root}: the
# `[[ -f ]]` guard, the `rg -q` nolint test, and the line count. A violating
# fixture returns before the `rg` line ever matters, so every subdirectory case
# above leaves that one path unpinned — drop its `${repo_root}/` prefix and, run
# from go/internal, rg cannot open the file. It prints an IO error, the
# `&& return 0` never fires, and a file the marker exempts is reported as a
# 501-line violation. From the repo root the same broken line looks perfect.
#
# So the two subdirectory fixtures are a pair: one expected to violate, one
# expected to be exempt. Only the exempt one reaches the `rg` test, and both of
# its observables are asserted, because the mutation moves both — rc flips 0 to 1
# and the committer gets rg's error on stderr.
test_nolint_marker_is_read_from_the_repo_root() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "go/internal/big/marked.go" "nolint:filelength"

	GATE_CWD="${repo_dir}/go/internal"
	run_gate "${repo_dir}" filecap "go/internal/big/marked.go"
	assert_rc 0 "a //nolint:filelength file stays exempt when the gate runs from a subdirectory"
	assert_silent "and rg's error on the unreadable path does not reach the committer"
}

test_non_go_file_is_ignored() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 900 "go/internal/big/notes.txt"
	run_gate "${repo_dir}" filecap "go/internal/big/notes.txt"
	assert_rc 0 "a non-.go file over the cap is ignored"
}

# First-party Go outside the go/ module. precommit-go.sh's header block calls
# this asymmetry deliberate: the hook stages every .go path in the repo, so
# `filecap` caps sdk/go and tools, while `filecap-all` and the CI plugin only
# look at go/. The local arm is the ONLY thing enforcing the repo's 500-line rule
# out here, which is why it must not be quietly narrowed. Both verdicts are
# asserted because the claim is about the difference between them. Every other
# fixture lives under go/, so `case "${f}" in go/*) ;; *) return 0 ;; esac` at
# the top of filecap_check_file passed every assertion predating this case.
test_outside_go_module_is_capped_locally_only() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "sdk/go/factschema/scratch.go"

	run_gate "${repo_dir}" filecap "sdk/go/factschema/scratch.go"
	assert_rc 1 "a 501-line .go file outside go/ is capped by filecap"
	assert_contains "sdk/go/factschema/scratch.go"
	assert_contains "501 lines"

	run_gate "${repo_dir}" filecap-all
	assert_rc 0 "filecap-all does not see that file, so the strictness is local-only by design"
}

# The plugin matches "/testdata/" against an ABSOLUTE path, so a repo-root
# testdata/ tree is exempt in CI. The hook passes repo-RELATIVE paths, where that
# leading separator is absent; without the `<seg>/*` alternatives the local gate
# would reject a file CI never even lints. Latent, not live: the repo-root
# testdata/ tree holds two .go files, both _test.go and both under the cap, and
# there is no repo-root generated/ or vendor/ at all. These fixtures are the only
# thing holding each alternative in place, so each gets its own written file and
# its own assertion. An earlier version asserted `generated/` with a path it
# never created, so filecap_check_file returned 0 at its `[[ -f ... ]]` guard
# before the skip logic ran: the assertion passed either way, and one pass line
# claimed all three segments.
test_leading_segment_is_exempt() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 501 "testdata/nornicdb/oversize.go"
	write_file "${repo_dir}" 501 "generated/oversize.go"
	write_file "${repo_dir}" 501 "vendor/example.com/dep/oversize.go"

	run_gate "${repo_dir}" filecap "testdata/nornicdb/oversize.go"
	assert_rc 0 "a leading testdata/ segment is exempt (501-line file on disk)"

	run_gate "${repo_dir}" filecap "generated/oversize.go"
	assert_rc 0 "a leading generated/ segment is exempt (501-line file on disk)"

	run_gate "${repo_dir}" filecap "vendor/example.com/dep/oversize.go"
	assert_rc 0 "a leading vendor/ segment is exempt (501-line file on disk)"
}

# A path the hook stages but that does not exist on disk (deleted in the same
# commit, say) must not be a violation. This is what the old generated/ case was
# accidentally testing; it gets its own assertion so the guard is on purpose.
test_missing_file_is_ignored() {
	local repo_dir
	new_repo
	repo_dir="${REPO_DIR}"
	write_file "${repo_dir}" 10 "go/internal/big/present.go"
	run_gate "${repo_dir}" filecap "go/internal/big/absent.go"
	assert_rc 0 "a staged path with no file on disk is ignored"
	# The exit code alone does not hold the `[[ -f ]]` guard in place. Delete the
	# guard and rg and awk both run against a path that is not there: rg reports
	# an IO error, awk says "can't open file" and prints no count, and the empty
	# count still compares as 0 — so rc stays 0 and the assertion above passes
	# against a broken script. What breaks is the committer's terminal, which is
	# why this silence check is a counted assertion and not a bare guard.
	assert_silent "and the gate stays silent, so no rg or awk error reaches the committer"
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
	assert_rc 1 "a final line without a trailing newline still counts (matches the plugin)"
	assert_contains "501 lines"
}

# ---------------------------------------------------------------------------
# The nolint exemption must be a DIRECTIVE, not a mention.
#
# golangci-lint honours //nolint only where the comment's text begins with
# `nolint:`. This check used to match the bare string anywhere in the file, so a
# file that merely explained the marker in prose was exempt locally and still
# capped in CI — lax in the one direction a hook whose job is agreeing with CI
# must not be.
#
# Both spellings a real directive can take are covered, because the common one
# is a trailing comment on the reported line and an earlier attempt at this
# check only matched a comment at line start.
# ---------------------------------------------------------------------------
test_nolint_must_be_a_directive_not_a_mention() {
	local repo_dir i
	new_repo
	repo_dir="${REPO_DIR}"

	write_file "${repo_dir}" 501 "go/internal/big/trailing.go" "nolint:filelength"
	run_gate "${repo_dir}" filecap "go/internal/big/trailing.go"
	assert_rc 0 "a trailing //nolint:filelength directive on the package line exempts the file"

	mkdir -p "${repo_dir}/go/internal/big"
	{
		printf '// SPDX-License-Identifier: MIT\n'
		printf 'package fixture\n'
		printf '// the repo uses //nolint:filelength on a few generated files\n'
		i=4
		while ((i <= 501)); do
			printf '// filler line %d\n' "${i}"
			i=$((i + 1))
		done
	} >"${repo_dir}/go/internal/big/prose.go"
	run_gate "${repo_dir}" filecap "go/internal/big/prose.go"
	assert_rc 1 "prose mentioning nolint:filelength mid-comment is not a directive and stays capped"
}

# ---------------------------------------------------------------------------
# The package clause must be found the way the compiler sees it.
#
# The plugin reports against the AST position, so the directive window ends at
# the REAL package clause. Locating it with a plain `^package` regex matches a
# decoy inside a /* */ block and stops the window short, capping a file that
# carries a valid directive -- a local rejection CI does not make. And a
# //nolint written inside a block comment is text, not a directive, so it must
# not exempt anything.
# ---------------------------------------------------------------------------
test_package_clause_is_found_past_commented_decoys() {
	local repo_dir i
	new_repo
	repo_dir="${REPO_DIR}"
	mkdir -p "${repo_dir}/go/internal/big"

	{
		printf '// SPDX-License-Identifier: MIT\n/*\npackage decoy\n*/\n'
		printf 'package fixture //nolint:filelength // deliberate\n'
		i=6
		while ((i <= 501)); do printf '// filler line %d\n' "${i}"; i=$((i + 1)); done
	} >"${repo_dir}/go/internal/big/decoy.go"
	run_gate "${repo_dir}" filecap "go/internal/big/decoy.go"
	assert_rc 0 "a package decoy inside a block comment does not hide the real directive"

	{
		printf '// SPDX-License-Identifier: MIT\n/*\npackage decoy //nolint:filelength\n*/\n'
		printf 'package fixture\n'
		i=6
		while ((i <= 501)); do printf '// filler line %d\n' "${i}"; i=$((i + 1)); done
	} >"${repo_dir}/go/internal/big/inside.go"
	run_gate "${repo_dir}" filecap "go/internal/big/inside.go"
	assert_rc 1 "a nolint written inside a block comment is text, not a directive"
}
