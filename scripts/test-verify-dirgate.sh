#!/usr/bin/env bash
# Test mirror and BITES proof for scripts/verify-dirgate.sh (issue #6054).
# Exercises the REAL CLI entry point (not a reimplementation of its logic)
# against isolated scratch git repos, so every assertion here proves the
# actual production script's behavior. No live runtime dependency
# (Postgres/NornicDB/Go build) -- pure filesystem + git + bash.
#
# Usage: scripts/test-verify-dirgate.sh
set -uo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
verify_script="${script_root}/verify-dirgate.sh"
# shellcheck source=lib/dirgate-core.sh
source "${script_root}/lib/dirgate-core.sh"

pass_count=0
fail_count=0

record_pass() { pass_count=$((pass_count + 1)); printf 'PASS: %s\n' "$1"; }
record_fail() {
	fail_count=$((fail_count + 1))
	printf 'FAIL: %s\n' "$1"
	[[ -n "${2:-}" ]] && printf '  %s\n' "$2"
}

# new_scratch_repo creates an isolated git repo with an empty go/ dir and
# prints its path. --all mode needs `git ls-files`, so every scratch tree
# is a real (throwaway) git repo, not just a directory.
new_scratch_repo() {
	local dir
	dir="$(mktemp -d)"
	git -C "${dir}" init -q
	git -C "${dir}" config user.email "dirgate-test@example.invalid"
	git -C "${dir}" config user.name "dirgate-test"
	mkdir -p "${dir}/go"
	printf '%s\n' "${dir}"
}

# write_numbered_files DIR N writes N distinct qualifying .go files.
write_numbered_files() {
	local dir="$1" n="$2" i
	mkdir -p "${dir}"
	for ((i = 0; i < n; i++)); do
		printf 'package fixture\n' > "${dir}/file$(printf '%04d' "${i}").go"
	done
}

# run_dirgate REPO ARGS... runs the real verify-dirgate.sh against a
# scratch repo built by new_scratch_repo, committing its current state
# first (--all reads from git's index, not the working tree). Sets
# DIRGATE_OUT (combined stdout+stderr) and DIRGATE_EXIT.
run_dirgate() {
	local repo="$1"
	shift
	git -C "${repo}" add -A >/dev/null 2>&1
	git -C "${repo}" commit -q -m "scratch fixture" --allow-empty >/dev/null 2>&1
	DIRGATE_OUT="$(DIRGATE_REPO_ROOT="${repo}" DIRGATE_GO_DIR="${repo}/go" \
		DIRGATE_GRANDFATHER_TSV="${DIRGATE_GRANDFATHER_TSV_OVERRIDE:-${script_root}/lib/dirgate-grandfather.tsv}" \
		bash "${verify_script}" "$@" 2>&1)"
	DIRGATE_EXIT=$?
}

assert_contains() {
	local haystack="$1" needle="$2" label="$3"
	if [[ "${haystack}" == *"${needle}"* ]]; then
		record_pass "${label}"
	else
		record_fail "${label}" "expected to find: ${needle}
got: ${haystack}"
	fi
}

assert_exit() {
	local got="$1" want="$2" label="$3"
	if [[ "${got}" -eq "${want}" ]]; then
		record_pass "${label}"
	else
		record_fail "${label}" "exit ${got}, want ${want}"
	fi
}

empty_grandfather_tsv() {
	local dir="$1"
	printf '# empty scratch grandfather ledger\n' > "${dir}/grandfather.tsv"
	printf '%s\n' "${dir}/grandfather.tsv"
}

# ---------------------------------------------------------------------------
# (e) GREEN on the real, untouched tree.
# ---------------------------------------------------------------------------
test_real_tree_is_green() {
	local out exit_code
	out="$(bash "${verify_script}" --all 2>&1)"
	exit_code=$?
	assert_exit "${exit_code}" 0 "real tree: dirgate --all is green"
	if [[ -n "${out}" ]]; then
		record_fail "real tree: dirgate --all has no output" "got: ${out}"
	else
		record_pass "real tree: dirgate --all has no output (no violations, no removable-grandfather notes)"
	fi
}

# ---------------------------------------------------------------------------
# (a) Seeded over-limit directory goes RED, naming the directory and BOTH
#     legal exits.
# ---------------------------------------------------------------------------
test_cap_violation_red_and_exits_named() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	write_numbered_files "${repo}/go/internal/sprawl" 41

	run_dirgate "${repo}" --files go/internal/sprawl/file0000.go
	assert_exit "${DIRGATE_EXIT}" 1 "seeded 41-file directory is RED"
	assert_contains "${DIRGATE_OUT}" "internal/sprawl" "RED message names the directory"
	assert_contains "${DIRGATE_OUT}" "41 non-test .go files" "RED message names the count"
	assert_contains "${DIRGATE_OUT}" "split it into a subpackage" "RED message names legal exit 1 (split)"
	assert_contains "${DIRGATE_OUT}" "//nolint:dirgate // <reason>" "RED message names legal exit 2 (nolint)"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

# ---------------------------------------------------------------------------
# (b) Naming violation goes RED, naming the sibling subpackage.
# ---------------------------------------------------------------------------
test_naming_violation_red_names_subpackage() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	mkdir -p "${repo}/go/internal/widget/gizmo"
	printf 'package gizmo\n' > "${repo}/go/internal/widget/gizmo/gizmo.go"
	printf 'package widget\n' > "${repo}/go/internal/widget/gizmo_helpers.go"

	run_dirgate "${repo}" --files go/internal/widget/gizmo_helpers.go
	assert_exit "${DIRGATE_EXIT}" 1 "seeded naming collision is RED"
	assert_contains "${DIRGATE_OUT}" "gizmo_helpers.go" "naming RED names the offending file"
	assert_contains "${DIRGATE_OUT}" "sibling subpackage \"gizmo\"" "naming RED names the sibling subpackage"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

# ---------------------------------------------------------------------------
# (c) Each legal exit turns a RED finding GREEN.
# ---------------------------------------------------------------------------
test_nolint_with_justification_turns_cap_green() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	write_numbered_files "${repo}/go/internal/sprawl2" 41
	printf 'package fixture //nolint:dirgate // intentionally oversized, tracked in #9999\n' \
		> "${repo}/go/internal/sprawl2/file0000.go"

	run_dirgate "${repo}" --files go/internal/sprawl2/file0000.go
	assert_exit "${DIRGATE_EXIT}" 0 "cap violation with a justified //nolint:dirgate is GREEN"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

test_bare_nolint_is_rejected() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	write_numbered_files "${repo}/go/internal/sprawl3" 41
	printf 'package fixture //nolint:dirgate\n' > "${repo}/go/internal/sprawl3/file0000.go"

	run_dirgate "${repo}" --files go/internal/sprawl3/file0000.go
	assert_exit "${DIRGATE_EXIT}" 1 "a BARE //nolint:dirgate (no justification) is NOT accepted"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

test_splitting_turns_cap_green() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	write_numbered_files "${repo}/go/internal/sprawl4" 41
	rm "${repo}/go/internal/sprawl4/file0040.go" # simulate splitting one file out

	run_dirgate "${repo}" --files go/internal/sprawl4/file0000.go
	assert_exit "${DIRGATE_EXIT}" 0 "dropping back to 40 files (simulated split) is GREEN"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

test_moving_the_file_turns_naming_green() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	mkdir -p "${repo}/go/internal/widget2/gizmo"
	printf 'package gizmo\n' > "${repo}/go/internal/widget2/gizmo/gizmo.go"
	printf 'package widget2\n' > "${repo}/go/internal/widget2/gizmo_helpers.go"
	rm "${repo}/go/internal/widget2/gizmo_helpers.go" # simulate the move
	printf 'package gizmo\n' > "${repo}/go/internal/widget2/gizmo/gizmo_helpers.go"

	run_dirgate "${repo}" --files go/internal/widget2/gizmo/gizmo_helpers.go
	assert_exit "${DIRGATE_EXIT}" 0 "moving the file into the subpackage (simulated) is GREEN"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

# ---------------------------------------------------------------------------
# (d) Grandfathered directory is GREEN at its pinned count; adding one file
#     goes RED.
# ---------------------------------------------------------------------------
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

test_removable_grandfather_note() {
	local repo tsv
	repo="$(new_scratch_repo)"
	mkdir -p "${repo}/go/internal/cleanedup"
	write_numbered_files "${repo}/go/internal/cleanedup" 5
	tsv="${repo}/grandfather.tsv"
	printf 'internal/cleanedup\t50\tirrelevant-once-shrunk\n' > "${tsv}"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="${tsv}"

	run_dirgate "${repo}" --all
	assert_exit "${DIRGATE_EXIT}" 0 "a fully-cleaned-up grandfathered directory is GREEN"
	assert_contains "${DIRGATE_OUT}" "no longer needs grandfathering" "gate nudges a stale grandfather row for removal"

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

main() {
	test_real_tree_is_green
	test_cap_violation_red_and_exits_named
	test_naming_violation_red_names_subpackage
	test_nolint_with_justification_turns_cap_green
	test_bare_nolint_is_rejected
	test_splitting_turns_cap_green
	test_moving_the_file_turns_naming_green
	test_grandfathered_directory
	test_removable_grandfather_note
	test_digest_helper

	printf '\ntests passed: %d/%d\n' "${pass_count}" "$((pass_count + fail_count))"
	[[ "${fail_count}" -eq 0 ]]
}

main
