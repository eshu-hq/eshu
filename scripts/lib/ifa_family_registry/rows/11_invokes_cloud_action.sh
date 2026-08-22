#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# invokes_cloud_action row (#5997). See ../../ifa_family_registry.sh for the
# schema and every array declaration this file assigns into. Sibling of
# 09_handles_route.sh (#5995) and 10_runs_in.sh (#6000) -- read
# 09_handles_route.sh's header comment first; the shared-handler rationale
# for blocker_kind/cell_kind is identical across all three rows and is not
# repeated in full here.

# runner_lease_hold: same production partition-lease seam as handles_route,
# keyed to this family's own projection domain.
IFA_FAMILY_BLOCKER_KIND[invokes_cloud_action]="runner_lease_hold"
# wait_stage=runner: this family's rows are tagged
# ProjectionDomain=DomainInvokesCloudAction="invokes_cloud_action"
# (go/internal/reducer/shared_projection.go:44,
# go/internal/reducer/invokes_cloud_action_intents.go:207) -- the
# shared_projection_intents.projection_domain column.
IFA_FAMILY_WAIT_STAGE[invokes_cloud_action]="runner"
IFA_FAMILY_WAIT_KEY[invokes_cloud_action]="invokes_cloud_action"
IFA_FAMILY_SHARED_CELL[invokes_cloud_action]=1

IFA_FAMILY_DRIVE_FN[invokes_cloud_action]="ifa_symbol_runtime_drive"
IFA_FAMILY_ASSERT_FN[invokes_cloud_action]="ifa_invokes_cloud_action_assert"
# SHARED cassette var across all three trio rows -- see 09_handles_route.sh.
IFA_FAMILY_CASSETTE_VAR[invokes_cloud_action]="symbol_runtime_cassette"
IFA_FAMILY_EXPECTED_VAR[invokes_cloud_action]="invokes_cloud_action_expected_edges"

# go/internal/storage/cypher/canonical_invokes_cloud_action_edges.go:22.
# Single relationship type; this anchor covers the family's whole write
# surface. The CloudAction node id ("cloud-action:" + action) is created
# inline by this same MERGE (its own ON CREATE clause).
IFA_FAMILY_ANCHOR[invokes_cloud_action]="MERGE (func)-[rel:INVOKES_CLOUD_ACTION]->(action)"
# custom: its baseline, graph-write-failure cell, and runner-lease
# kill/reclaim cell are hand-written in
# scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh.
IFA_FAMILY_CELL_KIND[invokes_cloud_action]="custom"

# NOT in drive_all_cassettes -- driven by this family's own cells via
# DRIVE_FN/CASSETTE_VAR above.
IFA_FAMILY_FAULT_SHARED_DRIVE[invokes_cloud_action]="0"

# No handler retry-baseline or handler Go-file field; see
# 09_handles_route.sh.

IFA_FAMILY_NAMES+=(invokes_cloud_action)
