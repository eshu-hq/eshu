#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# iam_escalation hand-derived pin (#6228). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/iamescalation/iam_escalation_materialization.go
# declares `FactLoader factload.FactLoader` as a struct field at :80, Handle
# rejects a nil one at :107, and passes it to factload.LoadFactsForKinds at
# :138. An ACCESS EXCLUSIVE lock on fact_records would therefore block a read
# this handler really performs -- the blocker_kind a future fault cell would
# take. No such cell exists yet.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming `iam_escalation_materialization` (:239), and corroborated by
# MaterializationDomainDefinition
# (go/internal/reducer/iamescalation/iam_escalation_materialization.go:36).
IFA_FAMILY_PIN_WAIT_KEY="iam_escalation_materialization"

# go/internal/storage/cypher/iam_escalation_edge_writer.go:40 reads
# `MERGE (p)-[rel:CAN_ESCALATE_TO]->(t)`. The anchor is matched against EXECUTED
# statement text; there is no %s to fill --
# CanonicalIAMEscalationEdgeUpsertCypher bakes the static CAN_ESCALATE_TO token
# directly into the template, so the executed text carries the literal.
# A `%s` form here would match no executed statement, the scripted
# graph-write fault would never fire, and the cell would report green having
# tested nothing.
#
# NOT IAM_ESCALATION -- that string appears nowhere in code. The
# iamEscalationEdgeLabel const IS "CAN_ESCALATE_TO" (it doubles as the
# relationship type and the statement-metadata tag), so the anchor is read
# off the template, never derived from the port or family name.
IFA_FAMILY_PIN_ANCHOR="MERGE (p)-[rel:CAN_ESCALATE_TO]->(t)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: custom, prospectively -- no fault cell file exists for this
# family yet, and the generic public dispatchers reject custom rows, so the
# fault gate cannot dispatch it. A future fault cell flips nothing here; the
# row already declares the custom dispatch shape.
IFA_FAMILY_PIN_CELL_KIND="custom"
