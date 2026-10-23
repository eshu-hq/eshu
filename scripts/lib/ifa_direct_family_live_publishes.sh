#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the publishes
# DIRECT-materialization family (#6228 publishes live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_code_taint_evidence.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain publishes` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_publishes_drive replays the publishes cassette
# (one scope: the fixture package-registry scope carrying the owning
# repositories, the owned package, the source hints, and the versions).
ifa_publishes_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive publishes \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_publishes_assert pins the three-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The three edges are the package-level exact hint's ownership of the
# Package node, the version-scoped exact hint's ownership of
# ver-team-api-2.0.0, and the publication pairing of ver-team-api-1.0.0
# against the package-level hint -- the same endpoint pair under both
# evidence sources is two edges, not one. A regression that resolved every
# hint against the first repository would still produce "some edges" and
# fail only against an exact set. The fixture's non-producers -- the
# tombstoned-only match (stale), the homepage kind (provenance-only,
# rejected), the unmatched remote (unresolved), and the two-remote match
# (ambiguous, never picked) -- must contribute nothing on either path; the
# builders classify those instead of fabricating edges, and this set is
# what holds them to that.
#
# The relationship type is PUBLISHES, read off the provenance writer's
# MERGE (repo)-[rel:PUBLISHES]->(target) templates (Package and
# PackageVersion twins). It is NOT derived from the port or family name.
ifa_publishes_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert publishes materialized edges (three-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain publishes \
		-expected "${expected_edges}"
}
