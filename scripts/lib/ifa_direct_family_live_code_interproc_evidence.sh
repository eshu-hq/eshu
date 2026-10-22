#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the code_interproc_evidence
# DIRECT-materialization family (#6228 code_interproc_evidence live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_crossplane_satisfied_by.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain code_interproc_evidence` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_code_interproc_evidence_drive replays the interproc-evidence cassette
# (one scope: the fixture repository carrying the node substrate plus the
# three findings).
ifa_code_interproc_evidence_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive code-interproc-evidence \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_code_interproc_evidence_assert pins the two-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The two edges are the credential to payment-capture flow (authenticate to
# chargeCard) and the http-request to search-exec flow (parseQuery to
# runQuery, the cloud-boundary finding carrying the two-step why-trail) --
# each endpoint the content.CanonicalEntityID Function uid the entities
# phase stamps, proving the join resolves rather than collapsing to one
# edge. A regression that resolved every finding against the first Function
# node would still produce "some edges" and fail only against an exact set.
# The fixture's non-producer -- the tombstoned legacy finding -- must
# contribute nothing; the writer's MATCH-MATCH only ever produces an edge
# both endpoints confirm, and this set is what holds it to that.
#
# The relationship type is TAINT_FLOWS_TO, read off the interproc evidence
# writer's MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: row.uid}]->(t). It is
# NOT derived from the port or family name.
ifa_code_interproc_evidence_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert code_interproc_evidence materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain code_interproc_evidence \
		-expected "${expected_edges}"
}
