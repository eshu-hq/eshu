#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the derived_from
# DIRECT-materialization family (#6228 derived_from live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_built_from.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain derived_from` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_derived_from_drive replays the derived-from cassette (one scope: the
# owning lineage-app repository).
ifa_derived_from_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive derived-from \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_derived_from_assert pins the two-edge exact set. Called twice per cell
# for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The two edges are the two children the owning repository built from its
# single declared base: sha256:c1... and sha256:c2... each fan out to one
# DERIVED_FROM edge on the sha256:b1... base, both with attribution_basis
# repository_single_base (the Dockerfile-only inference seam). A regression
# that promoted every exact digest to an edge regardless of build evidence
# would still produce "some edges" and fail only against an exact set. The
# fixture's non-producers -- the base digest itself (an image is never its
# own ancestor, or CVE lineage traversal would cycle) and the exact
# third-party digest the repository merely deploys (a runtime reference
# never confers derivation, and claiming it would fabricate CVE lineage for
# an unrelated image) -- must contribute nothing; the row builder drops a
# digest without build provenance rather than inventing an edge, and this
# set is what holds it to that.
#
# The relationship type is DERIVED_FROM, read off the provenance writer's
# MERGE template filled per row from the closed single-member derivedFrom
# vocabulary. It is NOT derived from the port or family name.
ifa_derived_from_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert derived_from materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain derived_from \
		-expected "${expected_edges}"
}
