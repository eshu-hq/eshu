#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# ec2_uses_profile row (#6228). See ../../ifa_family_registry.sh for
# the schema and every array declaration this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are unavailable to
# one.
#
# FAULT CELLS LANDED (#6228 fault half). scripts/verify-ifa-fault-injection.sh
# drives and asserts this row through
# scripts/lib/ifa_fault_injection_ec2_uses_profile_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with a
# co-location atomic group). cell_kind=custom below records the hand-written
# dispatch; specs/ci-gates.v1.yaml gives this family ifa-fault-injection
# triggers so an edit to its cassette or its writer re-runs the matrix that
# observes it.

# Hand-derived, and non-vacuous once the determinism gate drives it:
# EC2UsesProfileMaterializationHandler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/ec2usesprofile/ec2_uses_profile_materialization.go:115)
# and Handle refuses to run without it (:149) before passing it to
# factload.LoadFactsForKinds (:182). The handler reads fact_records AFTER
# claiming its work item, so an ACCESS EXCLUSIVE lock on that table would hold
# it genuinely in flight -- the blocker_kind this row's fault cell takes.
# Same table and same reasoning as rows/13; see that row for why
# fact_work_items is the wrong target.
IFA_FAMILY_BLOCKER_KIND[ec2_uses_profile]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[ec2_uses_profile]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# rows naming the same domain (:252-253), and corroborated by the domain
# definition (MaterializationDomainDefinition in
# go/internal/reducer/ec2usesprofile/ec2_uses_profile_materialization.go:35).
# Proven on a live stack by this row's own determinism drive, which leaves
# exactly one `ec2_uses_profile_materialization | succeeded` row per cell.
IFA_FAMILY_WAIT_KEY[ec2_uses_profile]="ec2_uses_profile_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[ec2_uses_profile]=1

IFA_FAMILY_DRIVE_FN[ec2_uses_profile]="ifa_ec2_uses_profile_drive"
IFA_FAMILY_ASSERT_FN[ec2_uses_profile]="ifa_ec2_uses_profile_assert"
IFA_FAMILY_CASSETTE_VAR[ec2_uses_profile]="ec2_uses_profile_cassette"
IFA_FAMILY_EXPECTED_VAR[ec2_uses_profile]="ec2_uses_profile_expected_edges"

# go/internal/storage/cypher/cloud/aws/ec2/uses_profile_edge_writer.go:47, which
# reads `MERGE (source)-[rel:%s]->(target)` in source. The anchor is
# matched against EXECUTED statement text, so it carries the filled form:
# CanonicalEC2UsesProfileEdgeUpsertCypherFormat's one %s is substituted per
# row from ec2UsesProfileRelationshipVocabulary, a closed single-member set
# screened by validateEC2UsesProfileRelationshipType, so the executed text
# carries the USES_PROFILE literal. Pinning a `%s` form here would match no
# executed statement and a scripted graph-write fault would never fire -- a
# green cell that tested nothing.
#
# It is NOT EC2_USES_PROFILE: that string is the ec2UsesProfileEdgeLabel
# const, statement metadata carried beside the query that never reaches the
# graph. The type is read off the template, never derived from the port or
# family name.
IFA_FAMILY_ANCHOR[ec2_uses_profile]="MERGE (source)-[rel:USES_PROFILE]->(target)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[ec2_uses_profile]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[ec2_uses_profile]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[ec2_uses_profile]="0"

IFA_FAMILY_HANDLER_GO_FILE[ec2_uses_profile]="go/internal/reducer/ec2usesprofile/ec2_uses_profile_materialization.go"

IFA_FAMILY_NAMES+=(ec2_uses_profile)
