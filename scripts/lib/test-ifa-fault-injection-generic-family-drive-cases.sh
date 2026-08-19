#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154
# Hermetic proof for the registry-sourced family drive hook and the family-scoped
# baseline key in scripts/lib/ifa_fault_generic_cells.sh.
#
# WHY THESE EXIST. drive_all_cassettes carries a fixed family set and the repo
# convention is that it is never extended, so a family outside it must drive its
# own cassette from inside the generic cells -- and must then compare against its
# OWN fault-free baseline, because the shared cell_baseline never drove that
# cassette. Get either half wrong and the failure is maximally confusing: a cell
# that drove nothing asserts an empty graph against a non-empty expected set, or
# a cell that drove correctly reports a graph divergence that is really a fixture
# difference.
#
# Both helpers read the registry through an accessor whose failure is
# indistinguishable from a "0" if tested inline, which is why they capture the rc
# and die. These cases pin that: an accessor failure and a junk value must both
# stop the cell, not pick a branch.
#
# Sourced by test-verify-ifa-fault-injection.sh; the parent owns strict mode,
# fail(), and ${generic_cells_lib}.

run_ifa_fault_injection_generic_family_drive_cases() {
	test_ifa_generic_drive_family_skips_shared_drive_families
	test_ifa_generic_drive_family_drives_self_driving_families
	test_ifa_generic_drive_family_rejects_accessor_failure
	test_ifa_generic_drive_family_rejects_junk_value
	test_ifa_generic_baseline_key_maps_both_directions
	test_ifa_generic_baseline_cell_refuses_a_shared_drive_family
}

# Shared harness: source the real helpers with the driver-owned globals stubbed.
_ifa_generic_family_drive_case_setup() {
	# shellcheck source=scripts/lib/ifa_fault_generic_cells.sh
	source "${generic_cells_lib}" 2>/dev/null || true
	drive_workers=1
	bin_dir="/nonexistent"
	log_dir="/nonexistent"
	# die() EXITS here, it does not return. The real die exits the gate, so a
	# stub that returned would let the helper run its next statement and this
	# module would prove the opposite of what it claims -- it would show the
	# guard "continuing" purely because the harness let it. Every case body is a
	# subshell, so exiting stops only that case.
	die() { printf 'die: %s\n' "$*" >&2; exit 1; }
}

# A family the shared drive already covers must NOT be driven again: a second
# drive doubles its fact_work_items rows and makes the retry-above-baseline
# comparison ill-defined.
test_ifa_generic_drive_family_skips_shared_drive_families() (
	_ifa_generic_family_drive_case_setup
	ifa_family_fault_shared_drive() { printf '1\n'; }
	ifa_family_registry_drive() { printf 'DROVE\n'; return 0; }

	local output rc=0
	output="$(_ifa_generic_drive_family testfamily testcell 2>&1)" || rc=$?
	[[ "${rc}" -eq 0 ]] || fail "drive hook failed for a shared-drive family (rc=${rc})"
	[[ "${output}" != *DROVE* ]] \
		|| fail "drive hook drove a family drive_all_cassettes already covers -- double-driving doubles its work rows"
)

# A family outside the shared drive MUST be driven, or its cell asserts an empty
# graph against a non-empty expected set.
test_ifa_generic_drive_family_drives_self_driving_families() (
	_ifa_generic_family_drive_case_setup
	ifa_family_fault_shared_drive() { printf '0\n'; }
	ifa_family_registry_drive() { printf 'DROVE\n'; return 0; }

	local output rc=0
	output="$(_ifa_generic_drive_family testfamily testcell 2>&1)" || rc=$?
	[[ "${rc}" -eq 0 ]] || fail "drive hook failed for a self-driving family (rc=${rc})"
	[[ "${output}" == *DROVE* ]] \
		|| fail "drive hook did not drive a family outside drive_all_cassettes -- its cell would assert an empty graph"
)

# An accessor that FAILS is not a "0". Testing the value inline would pick a
# branch on a lookup that never happened.
test_ifa_generic_drive_family_rejects_accessor_failure() (
	_ifa_generic_family_drive_case_setup
	ifa_family_fault_shared_drive() { return 1; }
	ifa_family_registry_drive() { printf 'DROVE\n'; return 0; }

	local output rc=0
	output="$(_ifa_generic_drive_family testfamily testcell 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "drive hook continued after the fault_shared_drive accessor failed -- it cannot know which branch is right"
	[[ "${output}" == *"accessor failed"* ]] \
		|| fail "drive hook did not name the accessor failure: ${output}"
	[[ "${output}" != *DROVE* ]] || fail "drive hook drove despite an accessor failure"
)

# Any value that is not literally 0 or 1 is a row defect, not a branch.
test_ifa_generic_drive_family_rejects_junk_value() (
	_ifa_generic_family_drive_case_setup
	ifa_family_fault_shared_drive() { printf 'yes\n'; }
	ifa_family_registry_drive() { printf 'DROVE\n'; return 0; }

	local output rc=0
	output="$(_ifa_generic_drive_family testfamily testcell 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "drive hook accepted a non-0/1 FAULT_SHARED_DRIVE value"
	[[ "${output}" == *"neither 0 nor 1"* ]] || fail "drive hook did not name the junk value: ${output}"
)

# The baseline key is the other half: a self-driving family compares against its
# own digest, a shared-drive family against the shared one. Both directions are
# asserted, because a helper that always returned "baseline" would satisfy a
# one-sided check while breaking exactly the families this work exists for.
test_ifa_generic_baseline_key_maps_both_directions() (
	_ifa_generic_family_drive_case_setup

	ifa_family_fault_shared_drive() { printf '1\n'; }
	local shared_key
	shared_key="$(_ifa_generic_baseline_key testfamily testcell)" \
		|| fail "baseline key resolution failed for a shared-drive family"
	[[ "${shared_key}" == "baseline" ]] \
		|| fail "a shared-drive family must compare against the shared baseline, got '${shared_key}'"

	ifa_family_fault_shared_drive() { printf '0\n'; }
	local own_key
	own_key="$(_ifa_generic_baseline_key testfamily testcell)" \
		|| fail "baseline key resolution failed for a self-driving family"
	[[ "${own_key}" == "baseline_testfamily" ]] \
		|| fail "a self-driving family must compare against its own baseline, got '${own_key}'"
)

# The baseline cell is only correct for a self-driving family. Dispatching it for
# one the shared drive covers would double-drive its cassette and mint a second,
# redundant digest, so it refuses rather than running.
test_ifa_generic_baseline_cell_refuses_a_shared_drive_family() (
	_ifa_generic_family_drive_case_setup
	ifa_family_fault_shared_drive() { printf '1\n'; }
	fresh_stack() { printf 'FRESH_STACK\n'; }

	local output rc=0
	output="$(cell_baseline_family testfamily 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "cell_baseline_family ran for a family drive_all_cassettes already covers"
	[[ "${output}" != *FRESH_STACK* ]] \
		|| fail "cell_baseline_family started a stack before refusing -- it must reject on the registry value alone"
)
