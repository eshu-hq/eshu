#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# iam_can_perform hand-derived pin (#6228). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/iamcan/iam_can_perform_materialization.go
# declares `FactLoader factload.FactLoader` as a struct field at :88, Handle
# rejects a nil one at :134, and passes it into the extraction path at :185. An ACCESS EXCLUSIVE lock
# on fact_records would therefore block a read this handler really performs --
# the blocker_kind a future fault cell would take. No such cell exists yet.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming `iam_can_perform_materialization`, and corroborated by
# PerformMaterializationDomainDefinition
# (go/internal/reducer/iamcan/iam_can_perform_materialization.go:36).
IFA_FAMILY_PIN_WAIT_KEY="iam_can_perform_materialization"

# go/internal/storage/cypher/iam_can_perform_edge_writer.go:47 reads
# `MERGE (p)-[rel:CAN_PERFORM]->(r)`. The anchor is matched against EXECUTED
# statement text; unlike iam_can_assume there is no %s to fill --
# CanonicalIAMCanPerformEdgeUpsertCypher bakes the static CAN_PERFORM token
# directly into the template, so the executed text carries the literal.
# A `%s` form here would match no executed statement, the scripted
# graph-write fault would never fire, and the cell would report green having
# tested nothing.
#
# NOT IAM_CAN_PERFORM -- that string appears nowhere in code. The
# iamCanPerformEdgeLabel const IS "CAN_PERFORM" (it doubles as the
# relationship type and the statement-metadata tag), so the anchor is read
# off the template, never derived from the port or family name.
IFA_FAMILY_PIN_ANCHOR="MERGE (p)-[rel:CAN_PERFORM]->(r)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: custom, prospectively -- no fault cell file exists for this
# family yet, and the generic public dispatchers reject custom rows, so the
# fault gate cannot dispatch it. A future fault cell flips nothing here; the
# row already declares the custom dispatch shape.
IFA_FAMILY_PIN_CELL_KIND="custom"
