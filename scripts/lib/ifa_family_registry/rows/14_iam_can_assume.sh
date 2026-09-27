#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# iam_can_assume row (#6228). See ../../ifa_family_registry.sh for
# the schema and every array declaration this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are unavailable to
# one. This row carries NO fault cells yet: the determinism gate drives it
# through the shared N={1,2,4} cell below, and the fault-injection area stays
# untouched (cell_kind=custom is rejected by the generic dispatchers, and no
# custom cell file exists, so the fault gate never dispatches this family).

# Hand-derived, and non-vacuous once the determinism gate drives it:
# IAMCanAssumeMaterializationHandler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/iamcan/iam_can_assume_materialization.go:83)
# and Handle refuses to run without it (:115) before passing it to the
# extraction path (:148). The handler reads fact_records AFTER claiming its work item, so
# an ACCESS EXCLUSIVE lock on that table would hold it genuinely in flight --
# the blocker_kind a future fault cell would take. Same
# table and same reasoning as rows/13; see that row for why fact_work_items is
# the wrong target.
IFA_FAMILY_BLOCKER_KIND[iam_can_assume]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[iam_can_assume]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming the same domain, and corroborated by the domain definition
# (AssumeMaterializationDomainDefinition in
# go/internal/reducer/iamcan/iam_can_assume_materialization.go:34). Proven on
# a live stack by this row's own determinism drive, which leaves exactly one
# `iam_can_assume_materialization | succeeded` row per cell.
IFA_FAMILY_WAIT_KEY[iam_can_assume]="iam_can_assume_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[iam_can_assume]=1

IFA_FAMILY_DRIVE_FN[iam_can_assume]="ifa_iam_can_assume_drive"
IFA_FAMILY_ASSERT_FN[iam_can_assume]="ifa_iam_can_assume_assert"
IFA_FAMILY_CASSETTE_VAR[iam_can_assume]="iam_can_assume_cassette"
IFA_FAMILY_EXPECTED_VAR[iam_can_assume]="iam_can_assume_expected_edges"

# go/internal/storage/cypher/iam_can_assume_edge_writer.go:43, which
# reads `MERGE (principal)-[rel:%s]->(role)` in source. The anchor is matched
# against EXECUTED statement text, so it carries the INTERPOLATED type: the %s
# is filled from iamCanAssumeRelationshipVocabulary, a closed single-member set
# screened per row, so the only value it ever takes is CAN_ASSUME. Pinning the
# literal `%s` here would match no executed statement and a scripted
# graph-write fault would never fire -- a green cell that tested nothing.
#
# It is NOT IAM_CAN_ASSUME. That string is iamCanAssumeEdgeLabel, statement
# metadata carried beside the query; it is not a relationship type and never
# appears in the graph.
IFA_FAMILY_ANCHOR[iam_can_assume]="MERGE (principal)-[rel:CAN_ASSUME]->(role)"
# custom, prospectively: no fault cell file exists for this family yet, and the
# generic public dispatchers reject `cell_kind=custom`, so the fault gate
# cannot dispatch it by accident. Flip only with a proven fault cell, never to
# generic without the lifecycle the generic header demands.
IFA_FAMILY_CELL_KIND[iam_can_assume]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[iam_can_assume]=""

# 0: drive_all_cassettes does not produce this family, and no fault cell
# drives it either; each determinism cell drives it through
# DRIVE_FN/CASSETTE_VAR instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[iam_can_assume]="0"

IFA_FAMILY_HANDLER_GO_FILE[iam_can_assume]="go/internal/reducer/iamcan/iam_can_assume_materialization.go"

IFA_FAMILY_NAMES+=(iam_can_assume)
