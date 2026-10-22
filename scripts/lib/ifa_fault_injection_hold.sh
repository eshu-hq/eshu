#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154  # Exported constants and driver-owned globals
# are consumed across sourced fault-injection libraries.
# Per-statement hold helpers for scripts/verify-ifa-fault-injection.sh
# (#8539 seam (2), hold-statement-before-delegation). Split out of
# scripts/lib/ifa_fault_injection_common.sh, which sits at the 500-line cap
# (same reason scripts/lib/ifa_fault_reclaim_assert.sh exists): writing a
# hold fault-script fixture and asserting the hold marker the in-binary
# decorator writes at engage time
# (go/internal/storage/cypher/fault/executor/hold.go).
#
# This file is a plain function library, not a script: it deliberately does
# NOT set `set -euo pipefail` (sourcing it would rebind the caller's shell
# options). The calling script owns strict mode, its own log/die helpers, and
# its own exit trap. Every function here returns a non-zero status and prints
# its own diagnostic to stderr; callers decide whether to `die`.

# Mirror prefix for the hold marker write failure
# (go/internal/storage/cypher/fault/executor/hold.go). The gate greps the
# reducer log for it, so drift makes the search silently find nothing --
# indistinguishable from "the marker write never failed". The mirror asserts the
# two match.
IFA_HOLD_MARKER_WRITE_FAILED_PREFIX="ifa fault: hold marker write failed"

# ifa_fault_write_hold_script writes a faultreplay v1 fault-script JSON
# fixture for exactly one hold-statement-before-delegation fault to path
# (#8539 seam (2)). operation_match is the Cypher substring the in-binary
# hold engages (go/internal/storage/cypher/fault/executor.FaultingExecutor.
# maybeHoldBeforeDelegation): the first Execute/ExecuteGroup/ExecutePhaseGroup
# call carrying a statement that contains it. Substring, not a fixed ordinal,
# for the same reason ifa_fault_write_once_script documents in
# scripts/lib/ifa_fault_injection_common.sh: an ordinal position is not
# stable across this gate's combined drive, but the edge-type MERGE text is
# a fixed, grep-stable anchor regardless of when it fires. A hold has no
# retry lane (it blocks before delegation, never returning a shaped error),
# so this helper -- like the Go-side Validate -- takes none.
ifa_fault_write_hold_script() {
	local path="$1" operation_match="$2"
	[[ -n "${path}" && -n "${operation_match}" ]] || {
		printf 'ifa_fault_write_hold_script: path and operation_match must be non-empty\n' >&2
		return 2
	}
	jq -n --arg operation_match "${operation_match}" '
		{
			"version": 1,
			"faults": [{
				"kind": "hold-statement-before-delegation",
				"trigger": {"operation_match": $operation_match}
			}]
		}
	' >"${path}"
}

# ifa_fault_assert_hold_marker proves the scripted hold engaged by reading
# the marker FaultingExecutor writes at the moment it engages
# (go/internal/storage/cypher/fault/executor/hold.go), rather than polling the
# reducer's captured stderr. Same log-race reasoning as
# ifa_fault_assert_once_fault_marker in
# scripts/lib/ifa_fault_injection_common.sh (#5974): the marker is written
# synchronously by the goroutine engaging the hold, before it blocks, so it
# cannot arrive late relative to the hold's own effects.
#
# A missing marker is a hard failure: it means the hold never engaged, which
# is exactly the inert-gate condition this assertion exists to catch. A
# present-but-wrong marker (rc=2) is distinguished deliberately, per #5974:
# it means the hold engaged on a different write than the anchor names --
# the injection works but the anchor is pointed at the wrong statement.
#
# Args: fault_script_path expected_operation_substring
ifa_fault_assert_hold_marker() {
	local script_path="$1" expected="$2"
	local marker="${script_path}.restart-sentinel.statement-held"
	if [[ ! -f "${marker}" ]]; then
		echo "ifa_fault_assert_hold_marker: no hold marker at ${marker} -- the scripted hold never engaged. An inert script, not a pass." >&2
		return 1  # 1 = absent; 2 = present but wrong operation. Callers must not conflate them.
	fi
	# Bash substring match, NOT an external tool (see the #5974 comment on
	# ifa_fault_assert_once_fault_marker in
	# scripts/lib/ifa_fault_injection_common.sh).
	local contents
	contents="$(cat "${marker}")" || {
		echo "ifa_fault_assert_hold_marker: could not read ${marker}; treat this as unknown, not as a verdict about the hold" >&2
		return 1
	}
	if [[ "${contents}" != *"${expected}"* ]]; then
		echo "ifa_fault_assert_hold_marker: marker ${marker} exists but does not name the targeted operation ${expected}; the hold engaged on a different write" >&2
		echo "--- marker contents ---" >&2
		printf '%s\n' "${contents}" >&2
		return 2
	fi
	return 0
}
