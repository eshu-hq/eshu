#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# handles_route row (#5995). See ../../ifa_family_registry.sh for the schema
# and every array declaration this file assigns into. Sibling rows: this
# file's own runs_in (#6000, 10_runs_in.sh) and invokes_cloud_action (#5997,
# 11_invokes_cloud_action.sh) -- all three share ONE cassette/drive_fn
# because their intent rows come from the
# SAME production entry point, buildSymbolRuntimeIntentRows
# (go/internal/reducer/symbol_runtime_refresh_intents.go:66), called inside
# CodeCallMaterializationHandler.Handle -- the same handler code_calls
# already covers (materialized_edge_family_blocker_shape_test.go:152-154's
# own exclusion reason).

# runner_lease_hold blocks ClaimPartitionLease on the production advisory
# key for this projection domain. It gives this family a distinct
# runner-stage kill/reclaim seam without reusing code_calls' first-stage
# handler wait key. The three-run live theory proof is recorded in
# docs/internal/evidence/6208-runner-lease-hold-live-theory.md.
IFA_FAMILY_BLOCKER_KIND[handles_route]="runner_lease_hold"
# wait_stage=runner, not handler: this family's intent rows are tagged
# ProjectionDomain=DomainHandlesRoute="handles_route"
# (go/internal/reducer/shared_projection.go:30,
# go/internal/reducer/handles_route_intents.go:100) -- the
# shared_projection_intents.projection_domain column, which is exactly what
# wait_stage=runner polls (ifa_family_registry.sh's wait_stage doc comment).
IFA_FAMILY_WAIT_STAGE[handles_route]="runner"
# Own family name, not code_calls' "code_call_materialization" -- see
# blocker_kind comment above for why the handler-stage domain would collide.
# "handles_route" IS the real shared_projection_intents.projection_domain
# value this family's rows carry (DomainHandlesRoute, cited above).
IFA_FAMILY_WAIT_KEY[handles_route]="handles_route"
# Plain reducer family (no maintenance pass), driven uniformly in the
# determinism gate's shared N={1,2,4} cell, mirroring shell_exec
# (08_shell_exec.sh:20-21) and code_calls (02_code_calls.sh:20).
IFA_FAMILY_SHARED_CELL[handles_route]=1

IFA_FAMILY_DRIVE_FN[handles_route]="ifa_symbol_runtime_drive"
IFA_FAMILY_ASSERT_FN[handles_route]="ifa_handles_route_assert"
# SHARED cassette var across all three trio rows -- one cassette, one
# builder pass (buildSymbolRuntimeIntentRows, cited above). NOT shared:
# assert_fn, expected_var, anchor.
IFA_FAMILY_CASSETTE_VAR[handles_route]="symbol_runtime_cassette"
IFA_FAMILY_EXPECTED_VAR[handles_route]="handles_route_expected_edges"

# go/internal/storage/cypher/canonical_handles_route_edges.go:19. Single
# relationship type; this anchor covers the family's whole write surface.
IFA_FAMILY_ANCHOR[handles_route]="MERGE (f)-[rel:HANDLES_ROUTE]->(e)"
# custom: this family's baseline, graph-write-failure cell, and runner-lease
# kill/reclaim cell are hand-written in
# scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh.
IFA_FAMILY_CELL_KIND[handles_route]="custom"

# NOT in drive_all_cassettes -- repo convention is that fixed set is never
# extended for a new family (ifa_fault_generic_cells.sh:137-142). This
# family's own cells drive the shared cassette through
# DRIVE_FN/CASSETTE_VAR above.
IFA_FAMILY_FAULT_SHARED_DRIVE[handles_route]="0"

# No handler retry-baseline or handler Go-file field is needed. This custom
# runner-stage cell proves recovery from durable shared intents and uses the
# partition-lease advisory key, not the first-stage handler lock.

IFA_FAMILY_NAMES+=(handles_route)
