#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# handles_route hand-derived pin (#5995). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that
# file's header before touching this one. Every value is HAND-TYPED, derived
# from the citations inline, never read back out of the registry row. Read
# scripts/lib/ifa_family_registry_pins/runs_in.sh and
# invokes_cloud_action.sh too -- the blocker_kind/cell_kind reasoning is
# shared across all three sibling rows, since they come from the same
# production entry point and are not repeated in full in every sibling file.

# blocker_kind=none: this family's rows are built by
# reducer.ExtractSymbolRuntimeIntentRows
# (go/internal/reducer/symbol_runtime_refresh_intents.go:49-56), called
# inside CodeCallMaterializationHandler.Handle -- the SAME handler
# code_calls already covers. A handler-stage blocker (shared_intent_lock)
# would need wait_key="code_call_materialization"
# (go/internal/reducer/defaults_domain_catalog.go:64 routes exactly
# DomainCodeCallMaterialization to this handler, and no other domain does),
# byte-identical to code_calls' own row
# (ifa_family_registry/rows/02_code_calls.sh:19) --
# TestIfaFamilyRegistryHandlerWaitKeysAreExclusive
# (go/internal/reducer/materialized_edge_family_blocker_shape_test.go:604-636)
# rejects two wait_stage=handler rows sharing one wait_key, and even absent
# that rejection the cell would observe/kill the SAME handler invocation
# cell_killworker_code_calls already proves -- not a distinct structural
# fact (this family is one of the six explicitly excluded from
# materializedEdgeFamilyBlockerLockstepExclusions,
# materialized_edge_family_blocker_shape_test.go:149-153, for exactly this
# reason). Recorded faithfully as none, matching sql_relationships' row for
# the identical shape (real fault coverage rests on
# cell_failgraphwrite_handles_route instead).
IFA_FAMILY_PIN_BLOCKER_KIND="none"
# wait_stage=runner: this family's intent rows are tagged
# ProjectionDomain=DomainHandlesRoute="handles_route"
# (go/internal/reducer/shared_projection.go:30,
# go/internal/reducer/handles_route_intents.go:100) -- the
# shared_projection_intents.projection_domain column, not a
# fact_work_items.domain value. blocker_kind=none carries no
# handler-required constraint (only shared_intent_lock does,
# materialized_edge_family_blocker_shape_test.go:580-582), so
# (none, runner) is legal.
IFA_FAMILY_PIN_WAIT_STAGE="runner"
# Own family name -- the real shared_projection_intents.projection_domain
# value (DomainHandlesRoute, cited above), not code_calls' handler-stage
# domain.
IFA_FAMILY_PIN_WAIT_KEY="handles_route"

# go/internal/storage/cypher/canonical_handles_route_edges.go:19. Single
# relationship type, so this anchor covers the family's whole write surface.
IFA_FAMILY_PIN_ANCHOR="MERGE (f)-[rel:HANDLES_ROUTE]->(e)"
# shared_cell: a plain reducer family needing no maintenance pass (same
# shape as shell_exec/code_calls), driven in the determinism gate's shared
# N={1,2,4} cell via a drive_fn shared with its two sibling rows.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites, not from what the generic
# dispatcher could express in principle. cell_killworker_family /
# cell_failgraphwrite_family both `die` for a cell_kind=custom family
# (ifa_fault_generic_cells.sh:404-412, 430-438); this family has no
# cell_killworker at all (see blocker_kind comment), so it is dispatched by
# name from scripts/verify-ifa-fault-injection.sh via its own hand-written
# cells in scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh.
IFA_FAMILY_PIN_CELL_KIND="custom"
