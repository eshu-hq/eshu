#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the s3_external_principal_grant
# DIRECT-materialization family (#6228 s3-grant live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_security_group_reachability.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file holds the pair's two asserts; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain s3_external_principal_grant` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_s3_external_principal_grant_drive replays the grant cassette
# (one scope: the four scanned bucket node facts the bucket-name join
# resolves against, plus the seven grant facts -- four edge producers and
# three deliberate non-producers).
ifa_s3_external_principal_grant_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive s3-external-principal-grant \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_s3_external_principal_grant_assert pins the four-edge exact set.
# Called twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The four edges are the cross-account grant on orders-artifacts (name-keyed
# bucket), the public grant on logs-archive, the service-principal grant on
# shared-reads (logging-s3.amazonaws.com), and the ARN-tail grant joining
# shared-reads with a blank bucket name (the source name derives from the
# ARN tail). A regression that resolved every grant against the first bucket
# would still produce "some edges" and fail only against an exact set. The
# fixture's three non-producers -- the ghost-bucket grant (source
# unresolved), the unsupported-kind grant (unprojectable principal), and the
# identity-less grant (missing identity) -- must contribute nothing on
# either path; the extractor classifies those instead of fabricating edges,
# and this set is what holds it to that. The idle bucket proves a scanned
# node alone never gains an edge.
#
# The relationship type is GRANTS_ACCESS_TO, read off the grant writer's
# MERGE (external_principal_grant_writer.go:34
# `MERGE (source)-[rel:%s]->(principal)`) filled from its closed
# single-member vocabulary. It is NOT derived from the port or family name.
ifa_s3_external_principal_grant_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert s3_external_principal_grant materialized edges (four-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain s3_external_principal_grant \
		-expected "${expected_edges}"
}
