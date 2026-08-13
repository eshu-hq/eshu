#!/usr/bin/env bash
# Per-file naming-exemption ledger cases for scripts/test-verify-dirgate.sh
# (scripts/lib/dirgate-naming-exempt.tsv), plus the removable-grandfather
# NOTE. Sourced by that test; not intended to run standalone. Relies on the
# harness (new_scratch_repo, run_dirgate, assert_contains, assert_exit,
# empty_grandfather_tsv, empty_naming_exempt_tsv, record_pass/record_fail),
# already sourced by the driver.

# ---------------------------------------------------------------------------
# (f) Naming exemption is pinned PER FILE (scripts/lib/dirgate-naming-exempt.tsv),
#     never gated by the directory's aggregate count against
#     scripts/lib/dirgate-grandfather.tsv. This is the primary regression
#     coverage for the #6054 follow-up defect: the old namingCovered gate
#     suppressed the WHOLE directory's naming check for as long as the live
#     count sat at or below the pinned cap, silently swallowing brand-new
#     naming violations.
# ---------------------------------------------------------------------------
test_naming_exempt_new_violation_below_pinned_count_is_red() {
	local repo cap_tsv naming_tsv
	repo="$(new_scratch_repo)"
	mkdir -p "${repo}/go/internal/legacy3/bar"
	printf 'package bar\n' > "${repo}/go/internal/legacy3/bar/bar.go"
	printf 'package legacy3\n' > "${repo}/go/internal/legacy3/bar_legacy.go"
	printf 'package legacy3\n' > "${repo}/go/internal/legacy3/bar_new.go"

	cap_tsv="${repo}/grandfather.tsv"
	# Pinned FileCount (50) sits well ABOVE the live count (2), as if other
	# files in this directory moved out elsewhere without touching this
	# row -- exactly the epic's move-issue (#6056-#6062) shape.
	printf 'internal/legacy3\t50\tirrelevant-because-live-count-is-below-the-pin\n' > "${cap_tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="${cap_tsv}"

	naming_tsv="${repo}/naming-exempt.tsv"
	printf 'internal/legacy3\tbar_legacy.go\n' > "${naming_tsv}"
	DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE="${naming_tsv}"

	run_dirgate "${repo}" --files go/internal/legacy3/bar_new.go
	assert_exit "${DIRGATE_EXIT}" 1 "a brand-new naming violation below the pinned cap is RED (the #6054 follow-up bug)"
	assert_contains "${DIRGATE_OUT}" "bar_new.go" "RED message names the new violation"
	assert_contains "${DIRGATE_OUT}" "sibling subpackage \"bar\"" "RED message names the sibling subpackage"
	if [[ "${DIRGATE_OUT}" == *"bar_legacy.go should move"* ]]; then
		record_fail "pinned bar_legacy.go must stay exempt" "got: ${DIRGATE_OUT}"
	else
		record_pass "pinned bar_legacy.go stays exempt while bar_new.go is reported"
	fi

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE
}

test_naming_exempt_pinned_file_stays_green() {
	local repo cap_tsv naming_tsv
	repo="$(new_scratch_repo)"
	mkdir -p "${repo}/go/internal/legacy4/bar"
	printf 'package bar\n' > "${repo}/go/internal/legacy4/bar/bar.go"
	printf 'package legacy4\n' > "${repo}/go/internal/legacy4/bar_legacy.go"

	cap_tsv="${repo}/grandfather.tsv"
	printf 'internal/legacy4\t50\tirrelevant-because-live-count-is-below-the-pin\n' > "${cap_tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="${cap_tsv}"

	naming_tsv="${repo}/naming-exempt.tsv"
	printf 'internal/legacy4\tbar_legacy.go\n' > "${naming_tsv}"
	DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE="${naming_tsv}"

	run_dirgate "${repo}" --files go/internal/legacy4/bar_legacy.go
	assert_exit "${DIRGATE_EXIT}" 0 "an already-pinned naming violation stays GREEN well below its directory's pinned cap"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE
}

# ---------------------------------------------------------------------------
# (g) A naming-exempt row that no longer matches the live tree (its file
#     was moved/renamed/removed) HARD FAILS --all, unlike the cap ledger's
#     soft NOTE -- forcing the row to be deleted in the same change.
# ---------------------------------------------------------------------------
test_naming_exempt_stale_row_hard_fails() {
	local repo naming_tsv
	repo="$(new_scratch_repo)"
	mkdir -p "${repo}/go/internal/legacy5/bar"
	printf 'package bar\n' > "${repo}/go/internal/legacy5/bar/bar.go"
	printf 'package legacy5\n' > "${repo}/go/internal/legacy5/unrelated.go"
	# bar_legacy.go does NOT exist -- as if it had already been moved/renamed
	# without the ledger row being deleted.

	naming_tsv="${repo}/naming-exempt.tsv"
	printf 'internal/legacy5\tbar_legacy.go\n' > "${naming_tsv}"
	DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE="${naming_tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"

	run_dirgate "${repo}" --all
	assert_exit "${DIRGATE_EXIT}" 1 "a naming-exempt row whose file no longer exists HARD FAILS --all"
	assert_contains "${DIRGATE_OUT}" "STALE naming-exempt row" "failure names it a stale exemption"
	assert_contains "${DIRGATE_OUT}" "bar_legacy.go" "failure names the stale file"
	assert_contains "${DIRGATE_OUT}" "no longer exists" "failure explains why it is stale"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE
}

# ---------------------------------------------------------------------------
# (h) A stale exemption must NEVER be read as covering a different,
#     never-pinned file that happens to collide with the same subpackage --
#     it must be reported on its own, AND the stale row must still fail.
# ---------------------------------------------------------------------------
test_naming_exempt_stale_row_does_not_cover_a_different_file() {
	local repo naming_tsv
	repo="$(new_scratch_repo)"
	mkdir -p "${repo}/go/internal/legacy6/bar"
	printf 'package bar\n' > "${repo}/go/internal/legacy6/bar/bar.go"
	# bar_legacy.go (the pinned name) is gone; bar_replacement.go is a
	# DIFFERENT, never-pinned file that also collides with "bar".
	printf 'package legacy6\n' > "${repo}/go/internal/legacy6/bar_replacement.go"

	naming_tsv="${repo}/naming-exempt.tsv"
	printf 'internal/legacy6\tbar_legacy.go\n' > "${naming_tsv}"
	DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE="${naming_tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"

	run_dirgate "${repo}" --all
	assert_exit "${DIRGATE_EXIT}" 1 "a stale exemption plus a new unpinned collision both fail --all"
	assert_contains "${DIRGATE_OUT}" "bar_replacement.go" "the new, never-pinned file is reported on its own"
	assert_contains "${DIRGATE_OUT}" "STALE naming-exempt row" "the stale row is separately reported for cleanup"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE
}

test_removable_grandfather_note() {
	local repo tsv
	repo="$(new_scratch_repo)"
	mkdir -p "${repo}/go/internal/cleanedup"
	write_numbered_files "${repo}/go/internal/cleanedup" 5
	tsv="${repo}/grandfather.tsv"
	printf 'internal/cleanedup\t50\tirrelevant-once-shrunk\n' > "${tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="${tsv}"
	# Isolate from the real committed naming-exempt ledger: --all also runs
	# dirgate_verify_naming_exempt_ledger, and this scratch repo's go/ tree
	# does not contain the real ledger's directories at all (which would
	# otherwise flag every real row as stale).
	DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE="$(empty_naming_exempt_tsv "${repo}")"

	run_dirgate "${repo}" --all
	assert_exit "${DIRGATE_EXIT}" 0 "a fully-cleaned-up grandfathered directory is GREEN"
	assert_contains "${DIRGATE_OUT}" "no longer needs grandfathering" "gate nudges a stale grandfather row for removal"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE
}
