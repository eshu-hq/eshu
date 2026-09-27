#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# iam_can_perform row (#6228). See ../../ifa_family_registry.sh for
# the schema and every array declaration this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are unavailable to
# one. This row carries NO fault cells yet: the determinism gate drives it
# through the shared N={1,2,4} cell below, and the fault-injection area stays
# untouched (cell_kind=custom is rejected by the generic dispatchers, and no
# custom cell file exists, so the fault gate never dispatches this family).

# Hand-derived, and non-vacuous once the determinism gate drives it:
# IAMCanPerformMaterializationHandler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/iamcan/iam_can_perform_materialization.go:88)
# and Handle refuses to run without it (:134) before passing it to the
# extraction path (:185). The handler reads fact_records AFTER claiming its work item, so
# an ACCESS EXCLUSIVE lock on that table would hold it genuinely in flight --
# the blocker_kind a future fault cell would take. Same
# table and same reasoning as rows/13; see that row for why fact_work_items is
# the wrong target.
IFA_FAMILY_BLOCKER_KIND[iam_can_perform]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[iam_can_perform]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming the same domain, and corroborated by the domain definition
# (PerformMaterializationDomainDefinition in
# go/internal/reducer/iamcan/iam_can_perform_materialization.go:36). Proven on
# a live stack by this row's own determinism drive, which leaves exactly one
# `iam_can_perform_materialization | succeeded` row per cell.
IFA_FAMILY_WAIT_KEY[iam_can_perform]="iam_can_perform_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[iam_can_perform]=1

IFA_FAMILY_DRIVE_FN[iam_can_perform]="ifa_iam_can_perform_drive"
IFA_FAMILY_ASSERT_FN[iam_can_perform]="ifa_iam_can_perform_assert"
IFA_FAMILY_CASSETTE_VAR[iam_can_perform]="iam_can_perform_cassette"
IFA_FAMILY_EXPECTED_VAR[iam_can_perform]="iam_can_perform_expected_edges"

# go/internal/storage/cypher/iam_can_perform_edge_writer.go:47, which
# reads `MERGE (p)-[rel:CAN_PERFORM]->(r)` in source. The anchor is matched
# against EXECUTED statement text; unlike iam_can_assume there is no %s to
# fill — CanonicalIAMCanPerformEdgeUpsertCypher bakes the static CAN_PERFORM
# token directly into the template (the granted action set lives in
# rel.actions, never in the relationship type), so the executed text carries
# the literal. Pinning a `%s` form here would match no executed statement and
# a scripted graph-write fault would never fire -- a green cell that tested
# nothing.
#
# It is NOT IAM_CAN_PERFORM: that string appears nowhere in code. The
# iamCanPerformEdgeLabel const IS "CAN_PERFORM" (it doubles as the
# relationship type and the statement-metadata tag), so the anchor is read
# off the template, never derived from the port or family name.
IFA_FAMILY_ANCHOR[iam_can_perform]="MERGE (p)-[rel:CAN_PERFORM]->(r)"
# custom, prospectively: no fault cell file exists for this family yet, and the
# generic public dispatchers reject `cell_kind=custom`, so the fault gate
# cannot dispatch it by accident. Flip only with a proven fault cell, never to
# generic without the lifecycle the generic header demands.
IFA_FAMILY_CELL_KIND[iam_can_perform]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[iam_can_perform]=""

# 0: drive_all_cassettes does not produce this family, and no fault cell
# drives it either; each determinism cell drives it through
# DRIVE_FN/CASSETTE_VAR instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[iam_can_perform]="0"

IFA_FAMILY_HANDLER_GO_FILE[iam_can_perform]="go/internal/reducer/iamcan/iam_can_perform_materialization.go"

IFA_FAMILY_NAMES+=(iam_can_perform)
