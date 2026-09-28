#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# s3_logs_to row (#6228). See ../../ifa_family_registry.sh for
# the schema and every array declaration this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are unavailable to
# one. This row carries NO fault cells yet: the determinism gate drives it
# through the shared N={1,2,4} cell below, and the fault-injection area stays
# untouched (cell_kind=custom is rejected by the generic dispatchers, and no
# custom cell file exists, so the fault gate never dispatches this family).

# Hand-derived, and non-vacuous once the determinism gate drives it:
# S3LogsToMaterializationHandler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/s3logsto/s3_logs_to_materialization.go:82)
# and Handle refuses to run without it (:114) before passing it to
# factload.LoadFactsForKinds (:145). The handler reads fact_records AFTER
# claiming its work item, so an ACCESS EXCLUSIVE lock on that table would hold
# it genuinely in flight -- the blocker_kind a future fault cell would take.
# Same table and same reasoning as rows/13; see that row for why
# fact_work_items is the wrong target.
IFA_FAMILY_BLOCKER_KIND[s3_logs_to]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[s3_logs_to]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming the same domain (:241), and corroborated by the domain
# definition (MaterializationDomainDefinition in
# go/internal/reducer/s3logsto/s3_logs_to_materialization.go:34).
# Proven on a live stack by this row's own determinism drive, which leaves
# exactly one `s3_logs_to_materialization | succeeded` row per cell.
IFA_FAMILY_WAIT_KEY[s3_logs_to]="s3_logs_to_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[s3_logs_to]=1

IFA_FAMILY_DRIVE_FN[s3_logs_to]="ifa_s3_logs_to_drive"
IFA_FAMILY_ASSERT_FN[s3_logs_to]="ifa_s3_logs_to_assert"
IFA_FAMILY_CASSETTE_VAR[s3_logs_to]="s3_logs_to_cassette"
IFA_FAMILY_EXPECTED_VAR[s3_logs_to]="s3_logs_to_expected_edges"

# go/internal/storage/cypher/s3_logs_to_edge_writer.go:43, which
# reads `MERGE (source)-[rel:%s]->(target)` in source. The anchor is
# matched against EXECUTED statement text, so it carries the filled form:
# CanonicalS3LogsToEdgeUpsertCypherFormat's one %s is substituted per
# row from s3LogsToRelationshipVocabulary, a closed single-member set
# screened by validateS3LogsToRelationshipType, so the executed text
# carries the LOGS_TO literal. Pinning a `%s` form here would match no
# executed statement and a scripted graph-write fault would never fire -- a
# green cell that tested nothing.
#
# It is NOT S3_LOGS_TO: that string is the s3LogsToEdgeLabel
# const, statement metadata carried beside the query that never reaches the
# graph. The type is read off the template, never derived from the port or
# family name.
IFA_FAMILY_ANCHOR[s3_logs_to]="MERGE (source)-[rel:LOGS_TO]->(target)"
# custom, prospectively: no fault cell file exists for this family yet, and the
# generic public dispatchers reject `cell_kind=custom`, so the fault gate
# cannot dispatch it by accident. Flip only with a proven fault cell, never to
# generic without the lifecycle the generic header demands.
IFA_FAMILY_CELL_KIND[s3_logs_to]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[s3_logs_to]=""

# 0: drive_all_cassettes does not produce this family, and no fault cell
# drives it either; each determinism cell drives it through
# DRIVE_FN/CASSETTE_VAR instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[s3_logs_to]="0"

IFA_FAMILY_HANDLER_GO_FILE[s3_logs_to]="go/internal/reducer/s3logsto/s3_logs_to_materialization.go"

IFA_FAMILY_NAMES+=(s3_logs_to)
