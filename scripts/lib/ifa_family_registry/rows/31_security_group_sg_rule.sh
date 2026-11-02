#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# security_group_sg_rule row (#6228 secgroup-pair live leaf, FIRST-LANDED).
# See ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read rows/13_iam_instance_profile_role.sh's header
# first -- it explains what a DIRECT-materialization family is and which
# blocker kinds are unavailable to one.
#
# FAULT CELLS LANDED (#6228 secgroup-pair live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_security_group_sg_rule_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# FIRST-LANDED tie-break (sibling precedent: rows/23_built_from.sh owns the
# container_image_identity handler-stage wait; rows/26_derived_from.sh takes
# the per-statement seam). Both pair families' edges are written by the ONE
# SecurityGroupReachabilityMaterializationHandler, and this row owns its
# handler-stage wait: the sibling security_group_rule_endpoint row declares
# wait_stage=statement on the same wait key as rendezvous only.
#
# Hand-derived, and non-vacuous now a fault cell exists:
# SecurityGroupReachabilityMaterializationHandler embeds
# `FactLoader factload.FactLoader`
# (go/internal/reducer/secgroup/security_group_reachability_materialization.go:121)
# and Handle refuses to run without it (:155-157) before passing it to
# factload.LoadFactsForKinds (:187-193), which pages over FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,103,130), so an
# ACCESS EXCLUSIVE lock on that table holds it genuinely in flight -- the
# blocker_kind this row's fault cell takes. Same table and same reasoning
# as rows/13; see that row for why fact_work_items is the wrong target.
#
# Triple readiness gate (unlike the s3 single-gate shape): the handler gates
# on THREE canonical-nodes-committed phases before its fact load
# (security_group_rule_uid, security_group_endpoint_uid, cloud_resource_uid --
# go/internal/storage/postgres/reducer_queue_readiness_sql.go:283-285),
# published by the security_group_rule_materialization,
# security_group_cidr_materialization, and aws_resource_materialization
# reducers the projector fans out from this same cassette
# (go/internal/projector/security/group_reachability_intents.go:35-75).
# So this row's kill cell banks all three phases through a prereq-scoped
# reducer before the lock goes on. Locking first deadlocks the cell by
# design: the claim gate never opens while the lock is held (proven live on
# the sibling iam_instance_profile_role domain).
IFA_FAMILY_BLOCKER_KIND[security_group_sg_rule]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[security_group_sg_rule]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from the projector intent
# (go/internal/projector/security/group_reachability_intents.go:71-75),
# and corroborated by the domain definition
# (DomainSecurityGroupReachabilityMaterialization in
# go/internal/reducer/contract/domain.go:146-147). Proven on a live stack by
# this row's own determinism drive, which leaves exactly one
# `security_group_reachability_materialization | succeeded` row per cell.
IFA_FAMILY_WAIT_KEY[security_group_sg_rule]="security_group_reachability_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[security_group_sg_rule]=1

IFA_FAMILY_DRIVE_FN[security_group_sg_rule]="ifa_security_group_reachability_drive"
IFA_FAMILY_ASSERT_FN[security_group_sg_rule]="ifa_security_group_sg_rule_assert"
IFA_FAMILY_CASSETTE_VAR[security_group_sg_rule]="security_group_reachability_cassette"
IFA_FAMILY_EXPECTED_VAR[security_group_sg_rule]="security_group_sg_rule_expected_edges"

# go/internal/storage/cypher/cloud/aws/security/group/security_reachability_edge_writer.go:87,
# which reads `MERGE (sg)-[rel:%s]->(rule)` in source. The anchor is matched
# against EXECUTED statement text as a SUBSTRING, so it carries the PREFIX
# form: the %s is substituted per row from the closed two-member direction
# vocabulary, so no single filled spelling matches every execution -- the
# observability_coverage rationale transfers (its `MERGE (obs)-[rel:AWS_COVERS_`
# prefix covers a five-member vocabulary the same way). No other production
# statement MERGEs `(sg)-[rel:`; the remaining spellings are retract DELETEs
# and prose.
IFA_FAMILY_ANCHOR[security_group_sg_rule]="MERGE (sg)-[rel:ALLOWS_"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[security_group_sg_rule]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[security_group_sg_rule]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[security_group_sg_rule]="0"

IFA_FAMILY_HANDLER_GO_FILE[security_group_sg_rule]="go/internal/reducer/secgroup/security_group_reachability_materialization.go"

IFA_FAMILY_NAMES+=(security_group_sg_rule)
