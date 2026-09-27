#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# iam_can_assume hand-derived pin (#6228). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/iamcan/iam_can_assume_materialization.go
# declares `FactLoader factload.FactLoader` as a struct field at :83, Handle
# rejects a nil one at :115, and passes it into the extraction path at :148. An ACCESS EXCLUSIVE lock
# on fact_records would therefore block a read this handler really performs --
# the blocker_kind a future fault cell would take. No such cell exists yet.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming `iam_can_assume_materialization`, and corroborated by
# AssumeMaterializationDomainDefinition
# (go/internal/reducer/iamcan/iam_can_assume_materialization.go:34).
IFA_FAMILY_PIN_WAIT_KEY="iam_can_assume_materialization"

# go/internal/storage/cypher/iam_can_assume_edge_writer.go:43 reads
# `MERGE (principal)-[rel:%s]->(role)`. The anchor is matched against EXECUTED
# statement text, so the type is pinned INTERPOLATED: the %s is filled from
# iamCanAssumeRelationshipVocabulary, a closed single-member set
# screened per row, so CAN_ASSUME is the only value it takes. A literal `%s`
# here would match no executed statement, the scripted graph-write fault would
# never fire, and the cell would report green having tested nothing.
#
# NOT IAM_CAN_ASSUME -- that is iamCanAssumeEdgeLabel,
# statement metadata beside the query, never a graph relationship type.
IFA_FAMILY_PIN_ANCHOR="MERGE (principal)-[rel:CAN_ASSUME]->(role)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: custom, prospectively -- no fault cell file exists for this
# family yet, and the generic public dispatchers reject custom rows, so the
# fault gate cannot dispatch it. A future fault cell flips nothing here; the
# row already declares the custom dispatch shape.
IFA_FAMILY_PIN_CELL_KIND="custom"
