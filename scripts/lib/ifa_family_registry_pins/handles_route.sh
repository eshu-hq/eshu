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

# runner_lease_hold: the custom kill cell blocks ClaimPartitionLease on the
# production advisory key for DomainHandlesRoute, then proves process death
# and replacement-runner recovery. See
# docs/internal/evidence/6208-runner-lease-hold-live-theory.md.
IFA_FAMILY_PIN_BLOCKER_KIND="runner_lease_hold"
# wait_stage=runner: this family's intent rows are tagged
# ProjectionDomain=DomainHandlesRoute="handles_route"
# (go/internal/reducer/shared_projection.go:30,
# go/internal/reducer/handles_route_intents.go:100) -- the
# shared_projection_intents.projection_domain column, not a
# fact_work_items.domain value.
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
# dispatcher could express in principle. This family's hand-written
# baseline, graph-write-failure, and runner-lease kill cells are dispatched
# by name from scripts/verify-ifa-fault-injection.sh.
IFA_FAMILY_PIN_CELL_KIND="custom"
