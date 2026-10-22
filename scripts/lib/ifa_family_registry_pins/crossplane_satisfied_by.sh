#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# crossplane_satisfied_by hand-derived pin (#6228 crossplane live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# go/internal/reducer/crossplane/satisfied_by_materialization.go declares
# `FactLoader factload.FactLoader` as a struct field at :144-148, Handle
# rejects a nil one at :177-179, and loads facts through loadEdgeFacts at
# :205. An ACCESS EXCLUSIVE lock on fact_records therefore blocks a read
# this handler really performs -- the blocker_kind this family's fault cell
# takes.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from BuildCrossplaneSatisfiedByMaterializationReducerIntent
# (go/internal/projector/crossplane/satisfaction/satisfied_by_intents.go:50),
# and corroborated by the domain definition
# (DomainCrossplaneSatisfiedByMaterialization in
# go/internal/reducer/contract/domain.go:57).
IFA_FAMILY_PIN_WAIT_KEY="crossplane_satisfied_by_materialization"

# go/internal/storage/cypher/canonical/crossplane/edges.go:33-42 reads
# `MERGE (claim)-[rel:SATISFIED_BY]->(xrd)`. The anchor is matched against
# EXECUTED statement text as a SUBSTRING, so it carries the template's own
# fixed spelling with no vocabulary substitution -- SATISFIED_BY is the only
# type that template can emit.
IFA_FAMILY_PIN_ANCHOR="MERGE (claim)-[rel:SATISFIED_BY]->(xrd)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh,
# dispatched by name (#6228 crossplane live leaf). Custom rather than generic
# for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
