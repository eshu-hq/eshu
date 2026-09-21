#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced by scripts/verify-ifa-fault-injection.sh,
# which owns ifa_det_pg and the compose/DSN globals these helpers read.
#
# Non-vacuity support for cell 3 (expire-lease-mid-handler).
#
# Cell 3 forces `claim_until = now()` on every claimed/running reducer row and
# then drains. It already proves rows were claimed BEFORE the expiry, but that
# is a precondition, not a result: it says nothing about whether the expiry
# actually caused a different worker to re-claim a row while the original
# handler was still in flight, which is the whole point of the cell. Without
# the assertion below, a green cell cannot be told apart from one whose
# handlers all finished before the next claim poll -- it would have tested
# nothing and still passed.
#
# This lives in its own file rather than beside ifa_fault_count_retried in
# ifa_fault_injection_common.sh because that file is at 498 of the repo's
# 500-line cap.
#
# Measured on a live stack before this assertion was added: the forced expiry
# re-claimed rows on every observed run, from a pre-expiry baseline of zero.
# That is why a STRICT increase is the right bar -- the signal is reliable, so
# a tolerant check would only hide a regression.

# ifa_fault_count_reclaimed counts reducer work items that have been claimed
# more than once, across every domain.
#
# Deliberately NOT domain-scoped, unlike ifa_fault_count_retried: cell 3
# expires the lease on every claimed/running reducer row rather than on one
# named domain, so whichever row the reducer happens to be holding is the one
# that gets re-claimed. A domain filter would miss the re-claim whenever that
# row belonged to a different domain, which is the same silent-zero failure the
# domain argument was added to ifa_fault_count_retried to avoid.
#
# It also does not require `status = 'succeeded'`. A second claim is proven by
# attempt_count alone; additionally demanding that the retried work finish is a
# different assertion, and one cell 3 already makes through its drain gate.
ifa_fault_count_reclaimed() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND attempt_count > 1;" \
		"${compose_file}" | tr -d '[:space:]'
}

# ifa_fault_assert_reclaimed_above proves the forced expiry genuinely caused a
# re-claim, by requiring the re-claimed row count to STRICTLY EXCEED the count
# captured immediately before the expiry. It prints the observed count on
# success so the cell can report a concrete number rather than a bare "ok".
#
# The baseline is captured per-run rather than assumed to be zero: cell 3 runs
# on a fresh stack, but a retry from any other cause before the expiry would
# otherwise be miscounted as this cell's evidence.
ifa_fault_assert_reclaimed_above() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local baseline="$5" budget="${6:-15}"
	local count
	for _ in $(seq 1 "${budget}"); do
		count="$(ifa_fault_count_reclaimed "${compose_project}" "${use_compose}" "${dsn}" "${compose_file}")"
		if [[ -n "${count}" && "${count}" -gt "${baseline}" ]]; then
			printf '%s' "${count}"
			return 0
		fi
		sleep 1
	done
	return 1
}
