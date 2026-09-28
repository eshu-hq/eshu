#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# ec2_uses_profile hand-derived pin (#6228). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/ec2usesprofile/ec2_uses_profile_materialization.go
# declares `FactLoader factload.FactLoader` as a struct field at :115, Handle
# rejects a nil one at :149, and passes it to factload.LoadFactsForKinds at
# :182. An ACCESS EXCLUSIVE lock on fact_records would therefore block a read
# this handler really performs -- the blocker_kind a future fault cell would
# take. No such cell exists yet.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# rows naming `ec2_uses_profile_materialization` (:252-253), and corroborated
# by MaterializationDomainDefinition
# (go/internal/reducer/ec2usesprofile/ec2_uses_profile_materialization.go:35).
IFA_FAMILY_PIN_WAIT_KEY="ec2_uses_profile_materialization"

# go/internal/storage/cypher/ec2_uses_profile_edge_writer.go:45 reads
# `MERGE (source)-[rel:%s]->(target)`. The anchor is matched against EXECUTED
# statement text, so it carries the filled form: the one %s is substituted
# per row from ec2UsesProfileRelationshipVocabulary, a closed single-member
# set screened by validateEC2UsesProfileRelationshipType, so the executed
# text carries the USES_PROFILE literal. A `%s` form here would match no
# executed statement, the scripted graph-write fault would never fire, and
# the cell would report green having tested nothing.
#
# NOT EC2_USES_PROFILE -- that string is the ec2UsesProfileEdgeLabel const,
# statement metadata carried beside the query that never reaches the graph.
# The type is read off the template, never derived from the port or family
# name.
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:USES_PROFILE]->(target)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: custom, prospectively -- no fault cell file exists for this
# family yet, and the generic public dispatchers reject custom rows, so the
# fault gate cannot dispatch it. A future fault cell flips nothing here; the
# row already declares the custom dispatch shape.
IFA_FAMILY_PIN_CELL_KIND="custom"
