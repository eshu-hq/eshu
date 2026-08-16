#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases,
# same as test-ifa-fault-injection-codeowners-cases.sh.
# Functional stub tests for the fault-injection lib's claimed-wait and
# once-fired-marker helpers, split out of test-verify-ifa-fault-injection.sh
# (mirroring the deployable-unit and review-cases splits) so that structural
# verifier stays under the repository's 500-line cap. The parent verifier
# owns strict mode, fail(), repo_root, and fault_lib
# (scripts/lib/ifa_fault_injection_common.sh); this module sources fault_lib
# itself since it is the first thing in the parent that needs the real
# ifa_fault_wait_for_claimed / ifa_fault_assert_once_fault_marker
# implementations rather than just grepping their text.

# run_ifa_fault_injection_marker_cases proves three things no static grep
# checks below it can: the claimed-wait SQL budget is validated before
# interpolation, the domain filter is applied only when a domain argument is
# passed (#5555), and ifa_fault_assert_once_fault_marker tells apart "no
# marker", "marker names the targeted write", and "marker names a DIFFERENT
# write" (#5974/#5555) -- the third case is the one that matters, since a
# marker alone only proves SOME fault fired.
run_ifa_fault_injection_marker_cases() {
	# The wait budget is interpolated into the server-side function call.
	# Reject a malformed environment override before it can reach psql.
	source "${fault_lib}"
	ifa_det_pg() { printf '1\n'; }
	if ifa_fault_wait_for_claimed test-project 1 test-dsn test-compose.yml '1; SELECT 1'; then
		fail "claimed-wait accepted a non-integer SQL budget"
	fi

	# Domain-scoping (#5555) actually changes the SQL: stub ifa_det_pg to
	# capture the query text it was asked to run, and assert the domain
	# clause is present only when a domain argument is passed.
	# ifa_fault_wait_for_claimed invokes ifa_det_pg inside a `$( ... )`
	# command substitution, which runs in a SUBSHELL -- a plain variable
	# assignment inside the stub would not survive back to this shell, so the
	# stub writes to a file instead.
	local capture_file
	capture_file="$(mktemp)"
	ifa_det_pg() { printf '%s' "$4" >"${capture_file}"; printf '1\n'; }
	ifa_fault_wait_for_claimed test-project 1 test-dsn test-compose.yml 5 "sql_relationship_materialization" >/dev/null
	rg --fixed-strings --quiet -- "AND domain = 'sql_relationship_materialization'" "${capture_file}" \
		|| fail "claimed-wait did not apply the domain filter when a domain argument was passed"
	: >"${capture_file}"
	ifa_fault_wait_for_claimed test-project 1 test-dsn test-compose.yml 5 >/dev/null
	if rg --fixed-strings --quiet -- "AND domain =" "${capture_file}"; then
		fail "claimed-wait applied a domain filter when no domain argument was passed"
	fi
	rm -f "${capture_file}"

	# ifa_fault_assert_once_fault_marker (#5974) is a real functional check,
	# not a string grep. It must tell three cases apart: the fault never
	# fired, it fired on the targeted write, and it fired on a DIFFERENT
	# write. The third is the one that matters -- a marker alone only proves
	# some fault fired.
	local marker_script sql_anchor
	# Not `local`: the EXIT trap below expands ${marker_dir} when the whole
	# script exits, long after this function has returned, so it must stay a
	# global to remain in scope under `set -u`.
	marker_dir="$(mktemp -d)"
	trap 'rm -rf "${marker_dir}"' EXIT
	marker_script="${marker_dir}/fault.json"
	sql_anchor="MERGE (source)-[rel:QUERIES_TABLE]->(target)"

	# Negative: no marker at all -- the inert-script case this assertion
	# exists for.
	if ifa_fault_assert_once_fault_marker "${marker_script}" "${sql_anchor}" 2>/dev/null; then
		fail "once-fired marker check passed with no marker present -- an inert script would report as a pass"
	fi

	# Positive: marker names the targeted operation.
	printf 'lane=queue-retry ordinal=3\noperation=%s SET rel.confidence = 0.95\n' "${sql_anchor}" \
		>"${marker_script}.restart-sentinel.once-fired"
	if ! ifa_fault_assert_once_fault_marker "${marker_script}" "${sql_anchor}"; then
		fail "once-fired marker check did not accept a marker naming the targeted SQL edge MERGE"
	fi

	# Negative: the fault fired, but on a different write. This is the
	# analogue of the split-evidence regression #5555 closed -- "a fault
	# fired" is not the same claim as "the fault hit the SQL family".
	printf 'lane=queue-retry ordinal=7\noperation=MERGE (r:CloudResource {uid: row.uid})\n' \
		>"${marker_script}.restart-sentinel.once-fired"
	if ifa_fault_assert_once_fault_marker "${marker_script}" "${sql_anchor}" 2>/dev/null; then
		fail "once-fired marker check passed on a fault that fired against CloudResource -- exactly the wrong-target defect #5555 exists to prevent"
	fi
}
