#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# runs_in hand-derived pin (#6000). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that
# file's header before touching this one, and read
# scripts/lib/ifa_family_registry_pins/handles_route.sh first: the
# blocker_kind/cell_kind reasoning is identical across all three sibling
# rows (handles_route #5995, runs_in #6000, invokes_cloud_action #5997) and
# is not repeated in full here.

# blocker_kind=none: same reasoning as handles_route.sh -- this family's rows
# come from reducer.ExtractSymbolRuntimeIntentRows, called inside
# CodeCallMaterializationHandler.Handle; a handler-stage blocker would need
# wait_key="code_call_materialization", byte-identical to code_calls' own
# row and rejected by TestIfaFamilyRegistryHandlerWaitKeysAreExclusive even
# before considering it proves nothing new. Real fault coverage rests on
# cell_failgraphwrite_runs_in instead.
IFA_FAMILY_PIN_BLOCKER_KIND="none"
# wait_stage=runner: this family's rows are tagged
# ProjectionDomain=DomainRunsIn="runs_in"
# (go/internal/reducer/shared_projection.go:38,
# go/internal/reducer/runs_in_intents.go:113) -- the
# shared_projection_intents.projection_domain column.
IFA_FAMILY_PIN_WAIT_STAGE="runner"
IFA_FAMILY_PIN_WAIT_KEY="runs_in"

# go/internal/storage/cypher/canonical_runs_in_edges.go:27. One intent row
# can fan out to N edges for N Workloads (no LIMIT in the live MATCH); the
# anchor still covers the family's whole write surface regardless of
# row-to-edge fan-out.
IFA_FAMILY_PIN_ANCHOR="MERGE (func)-[rel:RUNS_IN]->(workload)"
# shared_cell: a plain reducer family needing no maintenance pass, driven in
# the determinism gate's shared N={1,2,4} cell via a drive_fn shared with
# its two sibling rows.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind=custom: no cell_killworker exists for this family (see
# blocker_kind comment); its baseline and fail-graph-write cells are
# hand-written in scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh
# and dispatched by name.
IFA_FAMILY_PIN_CELL_KIND="custom"
