#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# iam_instance_profile_role row (#6228). See ../../ifa_family_registry.sh for
# the schema and every array declaration this file assigns into. Read
# rows/12_kubernetes_namespace_environment.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are unavailable to
# one. Both rows carry fault cells since #6309 (custom dispatch) and
# ifa-fault-injection triggers in specs/ci-gates.v1.yaml.

# Hand-derived, and non-vacuous once a fault cell exists:
# IAMInstanceProfileRoleMaterializationHandler embeds
# `FactLoader FactLoader`
# (go/internal/reducer/iam_instance_profile_role_materialization.go:57) and
# Handle refuses to run without it (:85) before passing it to the extraction
# path (:114). The handler reads fact_records AFTER claiming its work item, so
# an ACCESS EXCLUSIVE lock on that table holds it genuinely in flight. Same
# table and same reasoning as rows/12; see that row for why fact_work_items is
# the wrong target.
IFA_FAMILY_BLOCKER_KIND[iam_instance_profile_role]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[iam_instance_profile_role]="handler"
# The fact_work_items.domain the projector fans this family out under. Measured
# on a live stack, not read off a constant: driving the committed cassette and
# draining left exactly one
# `iam_instance_profile_role_materialization | succeeded` row.
IFA_FAMILY_WAIT_KEY[iam_instance_profile_role]="iam_instance_profile_role_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[iam_instance_profile_role]=1

IFA_FAMILY_DRIVE_FN[iam_instance_profile_role]="ifa_iam_instance_profile_role_drive"
IFA_FAMILY_ASSERT_FN[iam_instance_profile_role]="ifa_iam_instance_profile_role_assert"
IFA_FAMILY_CASSETTE_VAR[iam_instance_profile_role]="iam_instance_profile_role_cassette"
IFA_FAMILY_EXPECTED_VAR[iam_instance_profile_role]="iam_instance_profile_role_expected_edges"

# go/internal/storage/cypher/iam_instance_profile_role_edge_writer.go:22, which
# reads `MERGE (profile)-[rel:%s]->(role)` in source. The anchor is matched
# against EXECUTED statement text, so it carries the INTERPOLATED type: the %s
# is filled from iamInstanceProfileRoleRelationshipVocabulary, a closed
# single-member set screened per row, so the only value it ever takes is
# HAS_ROLE. Pinning the literal `%s` here would match no executed statement and
# the scripted graph-write fault would never fire -- a green cell that tested
# nothing.
#
# It is NOT IAM_INSTANCE_PROFILE_HAS_ROLE. That string is
# iamInstanceProfileRoleEdgeLabel, statement metadata carried beside the query;
# it is not a relationship type and never appears in the graph.
IFA_FAMILY_ANCHOR[iam_instance_profile_role]="MERGE (profile)-[rel:HAS_ROLE]->(role)"
# custom, same dispatch shape as rows/12: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[iam_instance_profile_role]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[iam_instance_profile_role]=""

# 0 on the same footing as rows/12: drive_all_cassettes does not produce this
# family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[iam_instance_profile_role]="0"

IFA_FAMILY_HANDLER_GO_FILE[iam_instance_profile_role]="go/internal/reducer/iam_instance_profile_role_materialization.go"

IFA_FAMILY_NAMES+=(iam_instance_profile_role)
