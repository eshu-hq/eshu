#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154
# Hermetic (mocked ifa_det_pg) proof for
# scripts/lib/ifa_fault_generic_table_lock.sh's
# _ifa_generic_require_table_domain_written -- the MANDATORY PRECONDITION
# ASSERT for the table_lock generic blocker mechanism. That file's own header
# says the mechanism is UNEXERCISED against a live stack in this PR
# (deployable_unit_edges, the only current table_lock family, is registered
# cell_kind=custom and keeps its own already-proven precondition instead) but
# claims to be "unit-tested (mocked ifa_det_pg)". This module is that proof:
# it drives every branch of the fail-closed discrimination the header
# describes -- query failure, empty output, and non-numeric output are all
# UNKNOWN and never read as a verdict in either direction; only a confirmed
# literal "0" is a real FAIL; any positive count is a real PASS -- without a
# live Postgres. Sourced by test-verify-ifa-fault-injection.sh; the parent
# owns strict mode, fail(), and repo_root.

# run_ifa_fault_injection_generic_table_lock_cases proves
# _ifa_generic_require_table_domain_written's fail-closed contract branch by
# branch: identifier rejection, query failure, empty output, non-numeric
# output, the sole real failure (a confirmed literal "0"), and a real pass
# (any positive count, including one that needs trimming).
run_ifa_fault_injection_generic_table_lock_cases() {
	test_ifa_generic_table_domain_written_rejects_bad_identifiers
	test_ifa_generic_table_domain_written_query_failure_is_unknown
	test_ifa_generic_table_domain_written_empty_output_is_unknown
	test_ifa_generic_table_domain_written_non_numeric_output_is_unknown
	test_ifa_generic_table_domain_written_zero_is_the_only_real_fail
	test_ifa_generic_table_domain_written_positive_count_passes
}

# The identifier-format guard must reject before ever issuing a query: a
# mock that dies loudly if invoked proves that ordering, rather than merely
# asserting the return code could also be produced by a query-side rejection.
test_ifa_generic_table_domain_written_rejects_bad_identifiers() (
	# shellcheck source=scripts/lib/ifa_fault_generic_table_lock.sh
	source "${table_lock_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn compose_file=test-compose.yml
	local rc output

	ifa_det_pg() { printf 'ifa_det_pg invoked with a rejected identifier\n' >&2; return 99; }

	rc=0
	output="$(_ifa_generic_require_table_domain_written testfamily 'Bad-Table' gooddomain 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "table-domain precondition accepted an invalid table identifier (rc=${rc})"
	[[ "${output}" == *"table and domain must match"* ]] \
		|| fail "table-domain precondition did not name the identifier-format guard for an invalid table"

	rc=0
	output="$(_ifa_generic_require_table_domain_written testfamily goodtable 'bad domain' 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "table-domain precondition accepted an invalid domain identifier (rc=${rc})"
	[[ "${output}" == *"table and domain must match"* ]] \
		|| fail "table-domain precondition did not name the identifier-format guard for an invalid domain"
)

# A failed query (e.g. connection refused, syntax error) is UNKNOWN, never a
# verdict: the exact query exit code propagates and the message says so.
test_ifa_generic_table_domain_written_query_failure_is_unknown() (
	# shellcheck source=scripts/lib/ifa_fault_generic_table_lock.sh
	source "${table_lock_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn compose_file=test-compose.yml
	local rc output

	ifa_det_pg() { return 9; }
	rc=0
	output="$(_ifa_generic_require_table_domain_written testfamily admission_decisions ifa_family_1 2>&1)" || rc=$?
	[[ "${rc}" -eq 9 ]] \
		|| fail "table-domain precondition did not propagate the exact query-failure exit code (got ${rc}, want 9)"
	[[ "${output}" == *"PRECONDITION query on admission_decisions FAILED (exit 9); treat as unknown, not as a verdict"* ]] \
		|| fail "table-domain precondition did not mark a query failure as unknown rather than a verdict"
)

# Empty query output (no rows/no output at all) is UNKNOWN, never read as
# zero.
test_ifa_generic_table_domain_written_empty_output_is_unknown() (
	# shellcheck source=scripts/lib/ifa_fault_generic_table_lock.sh
	source "${table_lock_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn compose_file=test-compose.yml
	local rc output

	ifa_det_pg() { printf ''; }
	rc=0
	output="$(_ifa_generic_require_table_domain_written testfamily admission_decisions ifa_family_1 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "table-domain precondition did not fail closed on empty query output (got rc=${rc})"
	[[ "${output}" == *"returned non-numeric output"* && "${output}" == *"treat as unknown, not as zero"* ]] \
		|| fail "table-domain precondition did not mark empty output as unknown rather than zero"
)

# Non-numeric query output (a driver/protocol error surfacing as text) is
# UNKNOWN, never read as zero.
test_ifa_generic_table_domain_written_non_numeric_output_is_unknown() (
	# shellcheck source=scripts/lib/ifa_fault_generic_table_lock.sh
	source "${table_lock_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn compose_file=test-compose.yml
	local rc output

	ifa_det_pg() { printf 'not-a-count\n'; }
	rc=0
	output="$(_ifa_generic_require_table_domain_written testfamily admission_decisions ifa_family_1 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "table-domain precondition did not fail closed on non-numeric query output (got rc=${rc})"
	[[ "${output}" == *"returned non-numeric output not-a-count;"* && "${output}" == *"treat as unknown, not as zero"* ]] \
		|| fail "table-domain precondition did not mark non-numeric output as unknown rather than zero"
)

# The ONLY real failure: a confirmed, well-formed literal "0" -- the table
# genuinely has no row for this domain after an unblocked drain. This must
# read as a real verdict ("PRECONDITION FAILED"), not as "unknown".
test_ifa_generic_table_domain_written_zero_is_the_only_real_fail() (
	# shellcheck source=scripts/lib/ifa_fault_generic_table_lock.sh
	source "${table_lock_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn compose_file=test-compose.yml
	local rc output

	ifa_det_pg() { printf '0\n'; }
	rc=0
	output="$(_ifa_generic_require_table_domain_written testfamily admission_decisions ifa_family_1 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "table-domain precondition did not fail on a confirmed literal zero (got rc=${rc})"
	[[ "${output}" == *"PRECONDITION FAILED: expected at least one admission_decisions row for domain=ifa_family_1"* ]] \
		|| fail "table-domain precondition's zero case did not report a real verdict"
	[[ "${output}" != *"treat as unknown"* ]] \
		|| fail "table-domain precondition's confirmed-zero verdict must not be phrased as unknown -- it is the one real fail"
)

# Any positive count is a real pass, including one that needs whitespace
# trimming before the numeric comparison.
test_ifa_generic_table_domain_written_positive_count_passes() (
	# shellcheck source=scripts/lib/ifa_fault_generic_table_lock.sh
	source "${table_lock_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn compose_file=test-compose.yml
	local rc output

	ifa_det_pg() { printf '3\n'; }
	rc=0
	output="$(_ifa_generic_require_table_domain_written testfamily admission_decisions ifa_family_1)" || rc=$?
	[[ "${rc}" -eq 0 ]] \
		|| fail "table-domain precondition rejected a genuine positive count (got rc=${rc})"
	[[ "${output}" == *"precondition confirmed: 3 admission_decisions row(s) for domain=ifa_family_1"* ]] \
		|| fail "table-domain precondition's pass case did not confirm the count, table, and domain"

	ifa_det_pg() { printf ' 5 \n'; }
	rc=0
	output="$(_ifa_generic_require_table_domain_written testfamily admission_decisions ifa_family_2)" || rc=$?
	[[ "${rc}" -eq 0 ]] \
		|| fail "table-domain precondition rejected a positive count needing whitespace trimming (got rc=${rc})"
	[[ "${output}" == *"precondition confirmed: 5 admission_decisions row(s) for domain=ifa_family_2"* ]] \
		|| fail "table-domain precondition did not trim surrounding whitespace before confirming the count"
)
