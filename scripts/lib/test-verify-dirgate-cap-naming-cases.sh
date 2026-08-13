#!/usr/bin/env bash
# Basic cap-violation, naming-violation, and escape-hatch cases for
# scripts/test-verify-dirgate.sh. Sourced by that test; not intended to run
# standalone. Relies on the harness (new_scratch_repo, run_dirgate,
# assert_contains, assert_exit, record_pass/record_fail) and on
# dirgate-core.sh's functions, both already sourced by the driver.

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

# The Go plugin matches the package line with
# strings.HasPrefix(strings.TrimSpace(line), "package "), so it tolerates
# leading whitespace. The bash mirror matched `package *` at column 0 only, so
# the two disagreed on a file that is perfectly valid Go: the plugin suppressed
# the finding and the mirror reported it (#6054 review finding). A divergence
# between the two implementations is the failure mode the mirror exists to
# prevent, so the shapes are pinned here.
test_nolint_on_indented_package_line_is_accepted() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	write_numbered_files "${repo}/go/internal/sprawlws" 41
	printf '  package fixture //nolint:dirgate // indented package line, still valid Go\n' \
		> "${repo}/go/internal/sprawlws/file0000.go"

	run_dirgate "${repo}" --files go/internal/sprawlws/file0000.go
	assert_exit "${DIRGATE_EXIT}" 0 "a justified //nolint:dirgate on an INDENTED package line is accepted, matching the Go plugin"

	rm -rf "${repo}"
	unset DIRGATE_GRANDFATHER_TSV_OVERRIDE
}

test_nolint_on_a_non_package_line_is_ignored() {
	local repo
	repo="$(new_scratch_repo)"
	DIRGATE_GRANDFATHER_TSV_OVERRIDE="$(empty_grandfather_tsv "${repo}")"
	write_numbered_files "${repo}/go/internal/sprawlnp" 41
	# Whitespace tolerance must not become "any line anywhere": a marker in a
	# function body or a string is not a package-line marker.
	printf 'package fixture\n\nfunc x() { _ = "//nolint:dirgate // not a package line" }\n' \
		> "${repo}/go/internal/sprawlnp/file0000.go"

	run_dirgate "${repo}" --files go/internal/sprawlnp/file0000.go
	assert_exit "${DIRGATE_EXIT}" 1 "a //nolint:dirgate that is NOT on the package line does not suppress the cap"

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
