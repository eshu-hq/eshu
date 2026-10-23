#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the observability_coverage
# DIRECT-materialization family (#6228 observability_coverage live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_code_interproc_evidence.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain observability_coverage` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_observability_coverage_drive replays the observability-coverage
# cassette (one scope: the owning aws observability account scope).
ifa_observability_coverage_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive observability-coverage \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_observability_coverage_assert pins the five-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The five edges are the one AWS_COVERS_<signal> edge per member of the
# writer's closed coverage-signal vocabulary (alarm, composite_alarm,
# dashboard, log_group, trace_sampling), each resolving its observability
# object against the scanned CloudResource target by ARN dimension. A
# regression that promoted every alarm to an edge regardless of a resolvable
# dimension would still produce "some edges" and fail only against an exact
# set. The fixture's non-producers -- the metric-name-only alarm (no
# resolvable dimension, rejected), the service-name X-Ray rule (derived
# provenance-only, never an edge), and the alarm naming an unscanned target
# (unresolved gap, never an edge) -- must contribute nothing; the classifier
# drops a row whose dimension resolves to no scanned target instead of
# inventing an edge, and this set is what holds it to that.
#
# The relationship types are AWS_COVERS_<signal>, read off the writer's MERGE
# template filled per row from the closed observabilityCoverageSignal
# vocabulary. They are NOT derived from the port or family name.
ifa_observability_coverage_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert observability_coverage materialized edges (five-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain observability_coverage \
		-expected "${expected_edges}"
}
