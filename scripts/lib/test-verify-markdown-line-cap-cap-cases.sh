#!/usr/bin/env bash
# Cap-rule cases for scripts/test-verify-markdown-line-cap.sh (issue #6187):
# the over-cap / at-cap / grow directions the acceptance criterion names,
# plus the skip rules and --files routing around them. Sourced by that
# driver, which provides new_scratch_repo, write_md_lines, write_ledger,
# run_mdcap and the assert_*/record_* helpers. Not runnable on its own.
#
# Sibling of scripts/lib/test-verify-dirgate-cap-naming-cases.sh.

# ─── the three directions #6187 requires ────────────────────────────────────

# RED: a file one line over the cap, with nothing pinning it, fails.
test_over_cap_file_is_red() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 501
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "over-cap file fails"
	assert_contains "${MDCAP_OUT}" "go/internal/thing/README.md has 501 lines, exceeding the 500-line cap" \
		"over-cap finding names the file and both counts"
	assert_contains "${MDCAP_OUT}" "evaluated 1 Markdown file(s)" \
		"over-cap run reports what it evaluated"
	rm -rf "${repo}"
}

# GREEN: exactly at the cap passes. 500 is allowed; 501 is not.
test_at_cap_file_is_green() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 500
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "at-cap (exactly 500) file passes"
	assert_not_contains "${MDCAP_OUT}" "exceeding the 500-line cap" \
		"at-cap file produces no finding"
	rm -rf "${repo}"
}

# GROWTH-RED: the point of pinning the COUNT rather than the filename. The
# file is grandfathered and still fails, because it grew.
test_grandfathered_file_that_grows_is_red() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 901
	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"900")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "grandfathered file that grew fails"
	assert_contains "${MDCAP_OUT}" "grew from its grandfathered 900 lines to 901" \
		"growth finding names the pin and the live count"
	rm -rf "${repo}"
}

# ─── everything around them ─────────────────────────────────────────────────

# The committed tree must be green, or the gate cannot land. This runs the
# real script against the real repo with its real ledger -- no scratch tree,
# no overrides -- so a ledger that does not match today's tree fails here.
test_real_tree_is_green() {
	local out exit_status
	out="$(bash "${verify_script}" --all 2>&1)"
	exit_status=$?
	assert_exit "${exit_status}" 0 "real tree passes the markdown cap"
	assert_not_contains "${out}" "evaluated 0 Markdown file(s)" \
		"real-tree run evaluated a non-zero number of files"
}

test_grandfathered_file_at_its_pin_is_green() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 900
	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"900")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "grandfathered file exactly at its pin passes"
	rm -rf "${repo}"
}

# A shrink passes with a NOTE rather than failing -- the deliberate
# divergence from dirgate, which hard-fails a shrink. See
# scripts/lib/markdown-line-cap-grandfather.tsv's header for why.
test_grandfathered_shrink_is_green_with_a_note() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 700
	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"900")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "grandfathered file that shrank still passes"
	assert_contains "${MDCAP_OUT}" "NOTE go/internal/thing/README.md is 700 lines, below its grandfathered 900" \
		"shrink prints a re-pin NOTE"
	rm -rf "${repo}"
}

# awk NR, not `wc -l`: a 501-line file whose last line has no newline is 501
# lines and must fail. `wc -l` reports 500 for it, which would read as
# exactly at the cap.
test_unterminated_final_line_is_counted() {
	local repo tsv wc_count
	repo="$(new_scratch_repo)"
	write_md_lines_unterminated "${repo}/go/internal/thing/README.md" 501
	wc_count="$(wc -l < "${repo}/go/internal/thing/README.md" | tr -d '[:space:]')"
	if [[ "${wc_count}" == "500" ]]; then
		record_pass "fixture really is the case wc -l undercounts (wc -l says 500)"
	else
		record_fail "fixture really is the case wc -l undercounts" "wc -l said ${wc_count}, want 500"
	fi
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "unterminated 501st line still fails the cap"
	assert_contains "${MDCAP_OUT}" "has 501 lines" "unterminated file counted as 501"
	rm -rf "${repo}"
}

# go/cmd/audit-preflight/testdata carries fixture Markdown whose length is an
# input to a test. Capping it would be wrong, so the skip is real behavior,
# not defensive coding.
test_testdata_markdown_is_skipped() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/cmd/thing/testdata/fixture.md" 900
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "900-line testdata Markdown is skipped"
	assert_contains "${MDCAP_OUT}" "evaluated 0 Markdown file(s)" \
		"skip is a skip, not a silent pass on an evaluated file"
	rm -rf "${repo}"
}

test_hidden_directory_markdown_is_skipped() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/.scratch/notes.md" 900
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "900-line Markdown under a hidden directory is skipped"
	rm -rf "${repo}"
}

test_non_markdown_file_is_skipped() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/notes.txt" 900
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --files go/internal/thing/notes.txt
	assert_exit "${MDCAP_EXIT}" 0 "a 900-line non-.md file is not this gate's business"
	assert_contains "${MDCAP_OUT}" "evaluated 0 Markdown file(s) from 1 path(s)" \
		"--files reports the skip honestly"
	rm -rf "${repo}"
}

test_files_mode_catches_an_over_cap_file() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 501
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --files go/internal/thing/README.md
	assert_exit "${MDCAP_EXIT}" 1 "--files (the pre-commit path) fails on an over-cap file"
	assert_contains "${MDCAP_OUT}" "evaluated 1 Markdown file(s) from 1 path(s)" \
		"--files reports the evaluated count"
	rm -rf "${repo}"
}

# A commit that touches no Markdown under go/ or docs/ evaluates nothing. That is
# legitimate, and indistinguishable from "checked everything, all clean"
# unless the count is printed -- the same trap verify-dirgate.sh --files
# closes.
test_files_mode_reports_zero_evaluated_paths() {
	local repo tsv
	repo="$(new_scratch_repo)"
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --files examples/thing.md README.md
	assert_exit "${MDCAP_EXIT}" 0 "paths outside go/ and docs/ are skipped"
	assert_contains "${MDCAP_OUT}" "evaluated 0 Markdown file(s) from 2 path(s)" \
		"a run that evaluated nothing says so"
	rm -rf "${repo}"
}

# The fix the gate asks for actually turns it green: split the document in
# two and both halves are under the cap.
test_splitting_the_file_turns_it_green() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 700
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "700-line file starts red"
	write_md_lines "${repo}/go/internal/thing/README.md" 350
	write_md_lines "${repo}/go/internal/thing/AGENTS.md" 350
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "splitting by audience turns it green"
	assert_contains "${MDCAP_OUT}" "evaluated 2 Markdown file(s)" \
		"both halves were evaluated, not just one"
	rm -rf "${repo}"
}
