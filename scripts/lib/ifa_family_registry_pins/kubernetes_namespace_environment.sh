#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# kubernetes_namespace_environment hand-derived pin (#6228). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/kubernetes_namespace_materialization.go declares
# `FactLoader FactLoader` as a struct field at :129, Handle rejects a nil one at
# :147, and passes it into the extraction path at :157. The handler therefore
# reads fact_records after claiming its work item, so an ACCESS EXCLUSIVE lock
# on that table blocks a read this handler really performs -- the non-vacuity
# condition for this blocker kind.
#
# This is a DIRECT-materialization family: the reducer writes it straight to a
# cypher writer with no shared_projection_intents row, so shared_intent_lock is
# not merely unattractive here, it has no table to lock.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from a live drain of the committed cassette (one row,
# `kubernetes_namespace_materialization | succeeded`) and corroborated by
# go/internal/reducer/kubernetes_namespace_materialization.go's
# DomainKubernetesNamespaceMaterialization.
IFA_FAMILY_PIN_WAIT_KEY="kubernetes_namespace_materialization"

# go/internal/storage/cypher/kubernetes_namespace_node_writer.go:90. The
# preceding line (:89) MERGEs the Environment node by name; the anchor names the
# RELATIONSHIP merge because that is the write the fault cell interrupts.
IFA_FAMILY_PIN_ANCHOR="MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh,
# dispatched by name (#6309). Not generic:
# scripts/lib/ifa_fault_generic_table_lock.sh's header records that the generic
# table_lock path has never run live and that its mandatory precondition
# assumes a `domain` column fact_records lacks.
IFA_FAMILY_PIN_CELL_KIND="custom"
