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
# already covers (materialized_edge_family_blocker_shape_test.go:151-153's
# own exclusion reason).

# blocker_kind=none, NOT shared_intent_lock: a handler-stage kill cell for
# this family would have to declare wait_key="code_call_materialization"
# (the ONLY routed fact_work_items.domain for this handler -- confirmed
# go/internal/reducer/defaults_domain_catalog.go:64 routes
# DomainCodeCallMaterialization to CodeCallMaterializationHandler and no
# other domain does), byte-identical to code_calls' own row
# (ifa_family_registry/rows/02_code_calls.sh:19) --
# TestIfaFamilyRegistryHandlerWaitKeysAreExclusive
# (materialized_edge_family_blocker_shape_test.go:604-636) rejects two
# wait_stage=handler rows sharing one wait_key, and even if it did not, the
# cell would observe/kill the SAME handler invocation code_calls' own
# cell_killworker_code_calls already proves -- not a distinct structural
# fact. Full design rationale (Design A, ruled after proving a stronger
# lock-based blocker was mechanically possible but out of scope -- see the
# "Disposition" section):
# docs/internal/evidence/5995-5997-6000-symbol-runtime-lock-theory.md.
# Recorded faithfully as none, matching sql_relationships' row
# (01_sql_relationships.sh:15) for the identical reason: this family's real
# fault coverage rests on its own cell_failgraphwrite_handles_route instead.
IFA_FAMILY_BLOCKER_KIND[handles_route]="none"
# wait_stage=runner, not handler: this family's intent rows are tagged
# ProjectionDomain=DomainHandlesRoute="handles_route"
# (go/internal/reducer/shared_projection.go:30,
# go/internal/reducer/handles_route_intents.go:100) -- the
# shared_projection_intents.projection_domain column, which is exactly what
# wait_stage=runner polls (ifa_family_registry.sh's wait_stage doc comment).
# TestIfaFamilyRegistryWaitStageAndKeyCohere
# (materialized_edge_family_blocker_shape_test.go:580-582) only forbids
# (shared_intent_lock, runner); blocker_kind=none carries no such
# constraint, so (none, runner) is legal.
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
# custom: this family has no cell_killworker (blocker_kind=none, no distinct
# handler-stage proof possible per the blocker_kind comment above), so it
# cannot be reached through cell_killworker_family / cell_failgraphwrite_family
# -- both die for a custom family by design (ifa_fault_generic_cells.sh:404-412,
# 430-438). Its baseline and fail-graph-write cells are hand-written in
# scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh.
IFA_FAMILY_CELL_KIND[handles_route]="custom"

# NOT in drive_all_cassettes -- repo convention is that fixed set is never
# extended for a new family (ifa_fault_generic_cells.sh:137-142). This
# family's own cells (cell_baseline_symbol_runtime,
# cell_failgraphwrite_handles_route) drive the shared cassette through
# DRIVE_FN/CASSETTE_VAR above.
IFA_FAMILY_FAULT_SHARED_DRIVE[handles_route]="0"

# No IFA_FAMILY_RETRY_BASELINE_VAR / IFA_FAMILY_HANDLER_GO_FILE: both are
# required only for blocker_kind=shared_intent_lock
# (_ifa_generic_require_retry_baseline /
# _ifa_generic_require_intent_writer, ifa_fault_generic_cells.sh), and this
# row declares blocker_kind=none. Neither reader is reached for a custom,
# none-blocker family (cell_baseline_symbol_runtime is hand-written, not
# cell_baseline_family, so it does not call
# ifa_family_retry_baseline_var either -- confirm this citation against
# that cell's own body before relying on it).

IFA_FAMILY_NAMES+=(handles_route)
