#!/usr/bin/env bash
# Grandfather-ledger monotonic-ratchet cases for scripts/test-verify-
# dirgate.sh: pinned-count GREEN, growth RED, the cap-nolint refusal, and the
# shrink-requires-repin rule. Sourced by that test; not intended to run
# standalone. Relies on the harness (new_scratch_repo, run_dirgate,
# assert_contains, assert_exit, record_pass/record_fail) and on
# dirgate-core.sh's functions (e.g. dirgate_digest), both already sourced by
# the driver.

# ---------------------------------------------------------------------------
# (d) Grandfathered directory is GREEN at its pinned count; adding one file
#     goes RED.
# ---------------------------------------------------------------------------
# A cap nolint sits on the directory's representative file and would suppress the
# cap for the whole directory forever. On a grandfathered directory that is the
# hollow-out path: one marker on internal/query's doc.go un-gates 880 files, and
# "split it into a subpackage" does not compile for query/reducer/projector/mcp
# until the acyclic boundary lands. So the hatch must be refused there.
test_grandfathered_cap_nolint_is_refused() {
	local repo tsv digest
	repo="$(new_scratch_repo)"
	write_numbered_files "${repo}/go/internal/legacy" 45
	local -a bases=()
	local p
	for p in "${repo}"/go/internal/legacy/*.go; do
		bases+=("$(basename "${p}")")
	done
	digest="$(dirgate_digest "${bases[@]:-}")"

	tsv="${repo}/grandfather.tsv"
	printf '# scratch\ninternal/legacy\t45\t%s\n' "${digest}" > "${tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="${tsv}"

	# Grow past the pin, then try to buy it off with a fully justified marker on
	# the representative file -- the exact move that would work on a
	# non-grandfathered directory.
	write_numbered_files "${repo}/go/internal/legacy" 46
	local rep="${repo}/go/internal/legacy/file0000.go"
	printf 'package legacy //nolint:dirgate // 46 files, splitting is blocked on the acyclic boundary\n' > "${rep}"

	run_dirgate "${repo}" --files go/internal/legacy/file0045.go
	assert_exit "${DIRGATE_EXIT}" 1 "a justified cap //nolint:dirgate does NOT suppress a grandfathered directory"
	assert_contains "${DIRGATE_OUT}" "will NOT suppress it" "refusal explains that nolint is not an exit here"
	assert_contains "${DIRGATE_OUT}" "dirgate-grandfather.tsv" "refusal names the reviewed pin bump as the exit"
}

test_grandfathered_directory() {
	local repo tsv digest
	repo="$(new_scratch_repo)"
	write_numbered_files "${repo}/go/internal/legacy" 45
	local -a bases=()
	local p
	for p in "${repo}"/go/internal/legacy/*.go; do
		bases+=("$(basename "${p}")")
	done
	digest="$(dirgate_digest "${bases[@]:-}")"

	tsv="${repo}/grandfather.tsv"
	printf '# scratch\ninternal/legacy\t45\t%s\n' "${digest}" > "${tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="${tsv}"

	run_dirgate "${repo}" --files go/internal/legacy/file0000.go
	assert_exit "${DIRGATE_EXIT}" 0 "grandfathered directory at its pinned count of 45 is GREEN"

	write_numbered_files "${repo}/go/internal/legacy" 46
	run_dirgate "${repo}" --files go/internal/legacy/file0045.go
	assert_exit "${DIRGATE_EXIT}" 1 "adding one file to a grandfathered directory (45 -> 46) is RED"
	assert_contains "${DIRGATE_OUT}" "grew from its grandfathered count of 45 to 46" "growth RED explains what changed"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

# ---------------------------------------------------------------------------
# (k) The #6054 P1 ratchet fix (codex review on PR #6081): a grandfathered
#     directory that SHRINKS below its pinned count, while still over the
#     40-file cap, must fail and name the exact re-pin command -- shrinking
#     used to be an unconditional, digest-free pass, which let a directory
#     shrink and then silently regrow up to (but not past) its original pin
#     without ever failing again.
# ---------------------------------------------------------------------------
test_grandfathered_shrink_requires_repin() {
	local repo tsv digest
	repo="$(new_scratch_repo)"
	write_numbered_files "${repo}/go/internal/legacy7" 45
	local -a bases=()
	local p
	for p in "${repo}"/go/internal/legacy7/*.go; do
		bases+=("$(basename "${p}")")
	done
	digest="$(dirgate_digest "${bases[@]:-}")"

	tsv="${repo}/grandfather.tsv"
	printf '# scratch\ninternal/legacy7\t45\t%s\n' "${digest}" > "${tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="${tsv}"

	# Shrink to 43 -- still over the 40-file cap, so still a cap offender,
	# but below its pinned count of 45 and with no matching digest.
	rm "${repo}/go/internal/legacy7/file0043.go" "${repo}/go/internal/legacy7/file0044.go"

	run_dirgate "${repo}" --files go/internal/legacy7/file0000.go
	assert_exit "${DIRGATE_EXIT}" 1 "shrinking a grandfathered directory below its pin (still over cap) is RED"
	assert_contains "${DIRGATE_OUT}" "shrunk from its grandfathered count of 45 to 43" "RED message explains the shrink"
	assert_contains "${DIRGATE_OUT}" "dirgate-digest internal/legacy7" "RED message names the exact digest command to run"
	assert_contains "${DIRGATE_OUT}" "dirgate-grandfather.tsv" "RED message names the ledger file to edit"
	assert_contains "${DIRGATE_OUT}" "generate-dirgate-grandfather-go.sh" "RED message names the regenerator to re-run"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

# ---------------------------------------------------------------------------
# (l) The shrink-requires-repin rule above does NOT apply once the
#     directory's real count drops to or below the 40-file cap entirely --
#     it is no longer a cap offender at all, and scripts/verify-dirgate.sh
#     --all only nudges that stale row toward removal (see
#     test_removable_grandfather_note), never fails it outright.
# ---------------------------------------------------------------------------
test_grandfathered_shrink_below_cap_needs_no_repin() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_numbered_files "${repo}/go/internal/legacy8" 38
	tsv="${repo}/grandfather.tsv"
	printf '# scratch\ninternal/legacy8\t45\tirrelevant-because-below-cap\n' > "${tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="${tsv}"

	run_dirgate "${repo}" --files go/internal/legacy8/file0000.go
	assert_exit "${DIRGATE_EXIT}" 0 "a grandfathered directory shrunk to or below the 40-file cap needs no re-pin"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}
