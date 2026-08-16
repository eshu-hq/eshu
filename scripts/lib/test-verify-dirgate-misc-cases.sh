#!/usr/bin/env bash
# Directory-named-*.go regular-file guards and the --digest helper case for
# scripts/test-verify-dirgate.sh. Sourced by that test; not intended to run
# standalone. Relies on the harness (new_scratch_repo, run_dirgate,
# assert_contains, write_numbered_files), already sourced by the driver.

# ---------------------------------------------------------------------------
# (i) A directory whose NAME ends in .go must never be counted as a
#     qualifying file -- dirgate_qualifying_files' glob (dir/*.go) matches
#     directories too, and only a `[[ -f ]]` regular-file guard (not `[[ -e
#     ]]`, which also accepts directories) excludes it. Mirrors the Go
#     side's qualifyingFiles unit test intent (dirgate_test.go's
#     TestQualifyingFiles: "a subdirectory named sub.go ... must only ever
#     list regular files, never directories").
# ---------------------------------------------------------------------------
test_qualifying_files_ignores_directory_named_dot_go() {
	local repo out
	repo="$(new_scratch_repo)"
	write_numbered_files "${repo}/go/internal/dirguard" 3
	mkdir -p "${repo}/go/internal/dirguard/sub.go"
	out="$(DIRGATE_GO_DIR="${repo}/go" bash "${verify_script}" --digest internal/dirguard)"
	assert_contains "${out}" "count	3" "a directory named sub.go is not counted as a qualifying file"
	rm -rf "${repo}"
}

# ---------------------------------------------------------------------------
# (j) The SAME regular-file guard must apply inside
#     dirgate_naming_subpackages' has_go check: a candidate subpackage
#     directory whose only "*.go"-shaped entry is ITSELF a directory (no
#     real .go file inside it) must not be treated as a real package --
#     otherwise an unrelated file that happens to collide with that
#     directory's name is falsely reported as a naming violation.
# ---------------------------------------------------------------------------
test_naming_subpackages_ignores_directory_named_dot_go() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	mkdir -p "${repo}/go/internal/nsguard/bar/sub.go"
	printf 'package nsguard\n' > "${repo}/go/internal/nsguard/bar_evidence.go"

	run_dirgate "${repo}" --files go/internal/nsguard/bar_evidence.go
	assert_exit "${DIRGATE_EXIT}" 0 \
		"bar/ has no real .go file (only a directory named sub.go inside it), so it is not a real subpackage and bar_evidence.go is not a false naming violation"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

test_digest_helper() {
	local repo out
	repo="$(new_scratch_repo)"
	write_numbered_files "${repo}/go/internal/probe" 3
	out="$(DIRGATE_GO_DIR="${repo}/go" bash "${verify_script}" --digest internal/probe)"
	assert_contains "${out}" "count	3" "digest helper prints the file count"
	rm -rf "${repo}"
}

# ---------------------------------------------------------------------------
# (k) --files must report HOW MANY directories it evaluated.
#
#     Zero evaluated directories is a legitimate outcome: a commit touching
#     only .go paths outside go/ (tools/, or the generated grandfather.go
#     itself) maps to no package directory, and must still exit 0. But a
#     zero-evaluation run and a run that checked everything and found it
#     clean produce the SAME exit code, so the count is the only thing that
#     tells them apart.
#
#     This matters because that exit code has been cited as evidence a
#     dirgate row was correct. A re-pin commit touches only the ledger and
#     the generated Go file, neither under go/, so the hook evaluates
#     nothing and passes -- even with the row set to a blatantly wrong
#     value. Printing the count makes such a citation self-refuting.
# ---------------------------------------------------------------------------
test_files_reports_zero_evaluated_directories() {
	local repo
	repo="$(new_scratch_repo)"
	write_numbered_files "${repo}/go/internal/counted" 3

	# Paths that are .go but outside go/ -- exactly a re-pin commit's file list.
	run_dirgate "${repo}" --files tools/golangci-lint-dirgate/grandfather.go
	assert_exit "${DIRGATE_EXIT}" 0 \
		"a .go path outside go/ maps to no package directory and stays green"
	assert_contains "${DIRGATE_OUT}" "evaluated 0 directories" \
		"a run that checked nothing says so, instead of looking like a clean pass"

	run_dirgate "${repo}" --files
	assert_contains "${DIRGATE_OUT}" "evaluated 0 directories" \
		"--files with no arguments reports zero rather than passing silently"

	rm -rf "${repo}"
}

test_files_reports_evaluated_directory_count() {
	local repo
	repo="$(new_scratch_repo)"
	write_numbered_files "${repo}/go/internal/counted" 3

	run_dirgate "${repo}" --files go/internal/counted/file1.go
	assert_contains "${DIRGATE_OUT}" "evaluated 1 directory" \
		"one changed file under go/ reports one evaluated directory"

	rm -rf "${repo}"
}
