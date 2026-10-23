#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the built_from DIRECT-materialization
# family (#6228 slice C live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND, since #6309, the fault
# gate through scripts/lib/ifa_fault_injection_sources.sh — always right after
# scripts/lib/ifa_direct_family_live.sh, whose _ifa_direct_family_drive helper
# below reuses. Split out rather than appended there because that file sits at
# the tooling line cap; the per-family REGISTRY ROWS stay separate, which is
# where the split that matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain built_from` flag out literally rather than
# taking the domain as a parameter. That is deliberate: the domain is the one
# thing a shared helper must not abstract away, because it is what makes this
# family's assertion distinguishable from its siblings'. A parameterized call
# would let one family's coverage stand in for another's.

# ifa_built_from_drive replays the built-from cassette.
ifa_built_from_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive built-from \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_built_from_assert pins the two-edge exact set. Called twice per cell
# for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The two edges are the exact-digest resolutions: the app and worker digests
# whose OCI configs declare the fixture repository as builder each fan out to
# one BUILT_FROM edge on the fixture repository. A regression that promoted
# every exact digest to an edge regardless of build evidence would still
# produce "some edges" and fail only against an exact set. The fixture's other
# three digests -- the exact third-party digest with no build evidence, the
# ghost-builder digest naming an unscanned repository, and the digest the
# repository merely deploys (a runtime reference never confers build
# provenance, #5796) -- must contribute nothing; the extractor drops a
# digest without build provenance rather than inventing an edge, and this
# set is what holds it to that.
#
# The relationship type is BUILT_FROM, read off the provenance writer's MERGE
# template filled per row from the closed single-member builtFrom vocabulary.
# It is NOT derived from the port or family name.
ifa_built_from_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert built_from materialized edges (two-edge exact set) ===\n' "${label}"
	# Scoped to the cassette's scope id (the cloud_resource -scopes
	# precedent): the shared cell graph also holds the derived_from
	# scope's BUILT_FROM edges with the same (prefix, stamp, endpoints)
	# shape -- the container-image handler emits BUILT_FROM for every
	# exact digest with build evidence in every scope while DERIVED_FROM
	# attribution needs the same signal -- which the unscoped scan counts
	# as extras. The fixture stays the family's own two edges.
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain built_from \
		-expected "${expected_edges}" \
		-scopes "git-repository-scope:repository:eshu-fixture-orders-api"
}
