#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# runs_in row (#6000). See ../../ifa_family_registry.sh for the schema and
# every array declaration this file assigns into. Sibling of
# 09_handles_route.sh (#5995) and 11_invokes_cloud_action.sh (#5997) -- read
# 09_handles_route.sh's header comment first; the shared-handler rationale
# for blocker_kind/cell_kind is identical across all three rows and is not
# repeated in full here.

# runner_lease_hold: same production partition-lease seam as handles_route,
# keyed to this family's own projection domain.
IFA_FAMILY_BLOCKER_KIND[runs_in]="runner_lease_hold"
# wait_stage=runner: this family's rows are tagged
# ProjectionDomain=DomainRunsIn="runs_in"
# (go/internal/reducer/shared_projection.go:38,
# go/internal/reducer/runs_in_intents.go:113) -- the
# shared_projection_intents.projection_domain column.
IFA_FAMILY_WAIT_STAGE[runs_in]="runner"
IFA_FAMILY_WAIT_KEY[runs_in]="runs_in"
IFA_FAMILY_SHARED_CELL[runs_in]=1

IFA_FAMILY_DRIVE_FN[runs_in]="ifa_symbol_runtime_drive"
IFA_FAMILY_ASSERT_FN[runs_in]="ifa_runs_in_assert"
# SHARED cassette var across all three trio rows -- see 09_handles_route.sh.
IFA_FAMILY_CASSETTE_VAR[runs_in]="symbol_runtime_cassette"
IFA_FAMILY_EXPECTED_VAR[runs_in]="runs_in_expected_edges"

# go/internal/storage/cypher/canonical_runs_in_edges.go:27. One intent row
# can fan out to N edges for N Workloads (no LIMIT in that file's MATCH);
# the anchor still covers the family's whole write surface -- fan-out
# changes row-to-edge count, not the MERGE template the fault decorator
# matches against.
IFA_FAMILY_ANCHOR[runs_in]="MERGE (func)-[rel:RUNS_IN]->(workload)"
# custom: its baseline, graph-write-failure cell, and runner-lease
# kill/reclaim cell are hand-written in
# scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh.
IFA_FAMILY_CELL_KIND[runs_in]="custom"

# NOT in drive_all_cassettes -- driven by this family's own cells via
# DRIVE_FN/CASSETTE_VAR above.
IFA_FAMILY_FAULT_SHARED_DRIVE[runs_in]="0"

# No handler retry-baseline or handler Go-file field; see
# 09_handles_route.sh.

IFA_FAMILY_NAMES+=(runs_in)
