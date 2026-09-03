#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# inheritance_edges hand-derived pin (#5996). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh, which owns the
# hand-authored-literal rule, the totality diff, and the comparison against
# scripts/lib/ifa_family_registry.sh -- read that file's header before touching
# this one. Every value below is HAND-TYPED literal text, derived by reading the
# citations inline; it is never sourced, generated, or read back out of the
# registry row.

# go/internal/reducer/inheritance/materialization.go:62 embeds
# `IntentWriter IntentWriter` as a struct field, and Handle calls
# h.IntentWriter.UpsertIntents(ctx, intentRows) at :175. So this handler really
# does write shared_projection_intents, and a lock on that table blocks a write
# it performs -- the non-vacuity condition checkFamilyBlockerLockstep
# (go/internal/reducer/materialized_edge_family_blocker_shape_test.go) enforces
# for this kind. Handler stage: the guard at :83 rejects any intent whose
# Domain is not DomainInheritanceMaterialization, so the work this family's kill
# cell must catch in flight is a fact_work_items row in that domain.
IFA_FAMILY_PIN_BLOCKER_KIND="shared_intent_lock"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# go/internal/reducer/intent.go:62 --
# `DomainInheritanceMaterialization Domain = "inheritance_materialization"`.
# NOT "inheritance_edges": that is the second-stage ProjectionDomain label
# (go/internal/reducer/shared_projection.go:21), which is also this family's
# registry name. wait_stage=handler polls fact_work_items, so the first-stage
# string is the correct one.
IFA_FAMILY_PIN_WAIT_KEY="inheritance_materialization"

# go/internal/storage/cypher/canonical_inheritance_edges.go:48 is the INHERITS
# write template. The family writes FOUR relationship types across two files --
# INHERITS, OVERRIDES and ALIASES here (:48, :58, :68) and IMPLEMENTS in
# canonical_implements_edges.go:13 -- and the anchor names one of them on
# purpose: the once-fault decorator fails a single write and the cell then
# proves recovery against the full expected set, so the other three types are
# covered by the assertion rather than by the injection. That same file's header
# (:21) warns the first MERGE "yields INHERITS alone and undercounts the family
# fourfold" if it is mistaken for the whole family -- which is a statement about
# COUNTING, not about anchoring.
IFA_FAMILY_PIN_ANCHOR="MERGE (child)-[rel:INHERITS]->(parent)"
# shared_cell: this is a plain reducer family. Unlike deployable_unit_edges it
# needs no bootstrap-index maintenance pass to materialize, so it is driven in
# the determinism gate's shared N={1,2,4} cell rather than a standalone one.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites, not from blocker_kind.
# shared_intent_lock IS a shape ifa_fault_generic_cells.sh's dispatcher builds,
# and this family is dispatched through cell_killworker_family /
# cell_failgraphwrite_family rather than by naming hand-written functions.
IFA_FAMILY_PIN_CELL_KIND="generic"
