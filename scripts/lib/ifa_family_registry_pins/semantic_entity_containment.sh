#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# semantic_entity_containment hand-derived pin (#8359, #6228 arbiter verdict
# (a)). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/code/semantic/materialization.go declares
# `FactLoader factload.FactLoader` as a struct field at :65, Handle rejects a
# nil one at :83, and passes it to factload.LoadFactsForKinds at :92. An
# ACCESS EXCLUSIVE lock on fact_records would therefore block a read this
# handler really performs -- the blocker_kind a future fault cell would take.
# No such cell exists yet.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from the domain definition (DomainSemanticEntityMaterialization in
# go/internal/reducer/contract/domain.go:145), corroborated by Handle's own
# domain check (go/internal/reducer/code/semantic/materialization.go:76).
IFA_FAMILY_PIN_WAIT_KEY="semantic_entity_materialization"

# go/internal/storage/cypher/semantic/entity/statements.go's per-label
# templates share one relationship shape (`MERGE (f)-[rel:CONTAINS]->(n)`,
# e.g. semanticFunctionUpsertCypher at :217-219). The anchor is matched
# against EXECUTED statement text, so it carries the filled relationship form
# with the static CONTAINS token. A label-alternation form would match no
# NornicDB-executed statement (measured 2026-10-17: alternations inside a
# relationship pattern match nothing on the pinned NornicDB), and a `%s`
# form would match no executed statement either -- a green cell that tested
# nothing.
#
# NOT SEMANTIC_ENTITY_CONTAINMENT -- that string is family metadata carried
# beside the query that never reaches the graph. The type is read off the
# template, never derived from the port or family name.
IFA_FAMILY_PIN_ANCHOR="MERGE (f)-[rel:CONTAINS]->(n)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell (#8359 verdict (a):
# shared cell, no dedicated cell).
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: custom, prospectively -- no fault cell file exists for this
# family yet, and the generic public dispatchers reject custom rows, so the
# fault gate cannot dispatch it. A future fault cell flips nothing here; the
# row already declares the custom dispatch shape.
IFA_FAMILY_PIN_CELL_KIND="custom"
