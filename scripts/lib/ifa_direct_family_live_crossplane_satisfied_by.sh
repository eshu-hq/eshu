#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the crossplane_satisfied_by
# DIRECT-materialization family (#6228 crossplane live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_cloud_resource_container_image.sh,
# whose _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain crossplane_satisfied_by` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_crossplane_satisfied_by_drive replays the satisfied-by cassette (two
# scopes: the claims repo and the platform XRD repo).
ifa_crossplane_satisfied_by_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive crossplane-satisfied-by \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_crossplane_satisfied_by_assert pins the two-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The two edges are k8s:claim-orders-db and k8s:claim-billing-db each
# joining (database.example.com, PostgreSQL) against the single
# xrd:postgresql XRD -- two claims, one XRD, proving the fan-out counts
# rather than collapsing to one edge. A regression that resolved every
# K8sResource against the first XRD would still produce "some edges" and
# fail only against an exact set. The fixture's non-producers -- the
# cache claim matching two XRDs (ambiguous, never a fabricated pick), the
# ordinary Deployment with no XRD in its group, the core-group ConfigMap
# with no group segment, the cluster-scoped XRD carrying no claim_kind,
# and the tombstoned retired claim -- must contribute nothing; the writer's
# MATCH-MATCH only ever produces an edge both endpoints confirm, and this
# set is what holds it to that.
#
# The relationship type is SATISFIED_BY, read off the crossplane edge
# writer's MERGE (claim)-[rel:SATISFIED_BY]->(xrd). It is NOT derived from
# the port or family name.
ifa_crossplane_satisfied_by_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert crossplane_satisfied_by materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain crossplane_satisfied_by \
		-expected "${expected_edges}"
}
