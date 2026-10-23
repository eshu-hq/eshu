#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the code_taint_evidence
# DIRECT-materialization family (#6228 code_taint_evidence live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_code_interproc_evidence.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain code_taint_evidence` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_code_taint_evidence_drive replays the taint-evidence cassette
# (one scope: the fixture repository carrying the node substrate plus the
# two findings).
ifa_code_taint_evidence_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive code-taint-evidence \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_code_taint_evidence_assert pins the two-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The two edges are the http-request to sql-exec flow in handlePayment and
# the queue-message to command-exec flow in refundPayment -- each source
# the CanonicalEntityID Function uid the entities phase stamps, proving
# the join resolves rather than collapsing to one edge. A regression that
# resolved every finding against the first Function node would still
# produce "some edges" and fail only against an exact set. The fixture's
# non-producer -- the tombstoned voidPayment finding -- must contribute
# nothing; the writer's MATCH-MATCH only ever produces an edge both
# endpoints confirm, and this set is what holds it to that.
#
# The relationship type is HAS_TAINT_EVIDENCE, read off the taint evidence
# writer's MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev). It is NOT derived from
# the port or family name.
ifa_code_taint_evidence_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert code_taint_evidence materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain code_taint_evidence \
		-expected "${expected_edges}"
}
