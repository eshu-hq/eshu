#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# s3_logs_to hand-derived pin (#6228). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/s3logsto/s3_logs_to_materialization.go
# declares `FactLoader factload.FactLoader` as a struct field at :82, Handle
# rejects a nil one at :114, and passes it to factload.LoadFactsForKinds at
# :145. An ACCESS EXCLUSIVE lock on fact_records would therefore block a read
# this handler really performs -- the blocker_kind a future fault cell would
# take. No such cell exists yet.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming `s3_logs_to_materialization` (:241), and corroborated
# by MaterializationDomainDefinition
# (go/internal/reducer/s3logsto/s3_logs_to_materialization.go:34).
IFA_FAMILY_PIN_WAIT_KEY="s3_logs_to_materialization"

# go/internal/storage/cypher/s3_logs_to_edge_writer.go:43 reads
# `MERGE (source)-[rel:%s]->(target)`. The anchor is matched against EXECUTED
# statement text, so it carries the filled form: the one %s is substituted
# per row from s3LogsToRelationshipVocabulary, a closed single-member
# set screened by validateS3LogsToRelationshipType, so the executed
# text carries the LOGS_TO literal. A `%s` form here would match no
# executed statement, the scripted graph-write fault would never fire, and
# the cell would report green having tested nothing.
#
# NOT S3_LOGS_TO -- that string is the s3LogsToEdgeLabel const,
# statement metadata carried beside the query that never reaches the graph.
# The type is read off the template, never derived from the port or family
# name.
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:LOGS_TO]->(target)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: custom, prospectively -- no fault cell file exists for this
# family yet, and the generic public dispatchers reject custom rows, so the
# fault gate cannot dispatch it. A future fault cell flips nothing here; the
# row already declares the custom dispatch shape.
IFA_FAMILY_PIN_CELL_KIND="custom"
