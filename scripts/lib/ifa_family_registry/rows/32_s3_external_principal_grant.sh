#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# s3_external_principal_grant row (#6228 s3-grant live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read rows/13_iam_instance_profile_role.sh's header
# first -- it explains what a DIRECT-materialization family is and which
# blocker kinds are unavailable to one.
#
# FAULT CELLS LANDED (#6228 s3-grant live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_s3_external_principal_grant_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# Hand-derived, and non-vacuous now a fault cell exists:
# S3ExternalPrincipalGrantMaterializationHandler embeds
# `FactLoader factload.FactLoader`
# (go/internal/reducer/s3grant/s3_external_principal_grant_materialization.go)
# and Handle refuses to run without it (:92-93) before passing it to
# factload.LoadFactsForKinds (:119), which pages over FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,103,130), so an
# ACCESS EXCLUSIVE lock on that table holds it genuinely in flight -- the
# blocker_kind this row's fault cell takes. Same table and same reasoning
# as rows/19 (s3_logs_to, the sibling S3 direct family); see that row for
# why fact_work_items is the wrong target.
#
# Single readiness gate (the s3 shape, not the secgroup triple-gate one):
# this domain's claim is gated on canonical_nodes_committed/cloud_resource_uid
# (go/internal/storage/postgres/reducer_queue_readiness_sql.go:276),
# published by the aws_resource_materialization reducer during Handle --
# which reads fact_records. So this row's kill cell banks readiness through
# a prereq-scoped reducer before the lock goes on. Locking first deadlocks
# the cell by design (proven live on the sibling iam_instance_profile_role
# domain).
IFA_FAMILY_BLOCKER_KIND[s3_external_principal_grant]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[s3_external_principal_grant]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from the projector intent
# (go/internal/projector/cloud/aws/s3/external_principal_grant_materialization_intents.go:30),
# and corroborated by the domain definition
# (DomainS3ExternalPrincipalGrantMaterialization in
# go/internal/reducer/contract/domain.go:125-126). Proven on a live stack by
# this row's own determinism drive, which leaves exactly one
# `s3_external_principal_grant_materialization | succeeded` row per cell.
IFA_FAMILY_WAIT_KEY[s3_external_principal_grant]="s3_external_principal_grant_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[s3_external_principal_grant]=1

IFA_FAMILY_DRIVE_FN[s3_external_principal_grant]="ifa_s3_external_principal_grant_drive"
IFA_FAMILY_ASSERT_FN[s3_external_principal_grant]="ifa_s3_external_principal_grant_assert"
IFA_FAMILY_CASSETTE_VAR[s3_external_principal_grant]="s3_external_principal_grant_cassette"
IFA_FAMILY_EXPECTED_VAR[s3_external_principal_grant]="s3_external_principal_grant_expected_edges"

# go/internal/storage/cypher/cloud/aws/s3/external_principal_grant_writer.go:34,
# which reads `MERGE (source)-[rel:%s]->(principal)` in source. The anchor is
# matched against EXECUTED statement text, so it carries the filled form:
# the one %s is substituted per row from a closed single-member vocabulary
# (GRANTS_ACCESS_TO), so the executed text carries the literal. Pinning a
# `%s` form here would match no executed statement and a scripted
# graph-write fault would never fire -- a green cell that tested nothing.
# The s3_logs_to rationale transfers verbatim. It is NOT a grant label
# const: statement metadata beside the query never reaches the graph. The
# type is read off the template, never derived from the port or family name.
IFA_FAMILY_ANCHOR[s3_external_principal_grant]="MERGE (source)-[rel:GRANTS_ACCESS_TO]->(principal)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[s3_external_principal_grant]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[s3_external_principal_grant]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[s3_external_principal_grant]="0"

IFA_FAMILY_HANDLER_GO_FILE[s3_external_principal_grant]="go/internal/reducer/s3grant/s3_external_principal_grant_materialization.go"

IFA_FAMILY_NAMES+=(s3_external_principal_grant)
