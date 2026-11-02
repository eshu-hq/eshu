#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# security_group_rule_endpoint row (#6228 secgroup-pair live leaf, SECOND).
# See ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read rows/13_iam_instance_profile_role.sh's header
# first -- it explains what a DIRECT-materialization family is and which
# blocker kinds are unavailable to one.
#
# FAULT CELLS LANDED (#6228 secgroup-pair live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_security_group_rule_endpoint_cells.sh
# (baseline/failgraphwrite/kill trio with the per-statement hold kill,
# dispatched in shard order with a co-location atomic group).
# cell_kind=custom below records the hand-written dispatch;
# specs/ci-gates.v1.yaml gives this family ifa-fault-injection triggers so
# an edit to its cassette or its writer re-runs the matrix that observes it.
#
# PER-STATEMENT HOLD KILL CELL (sibling precedent: rows/26_derived_from.sh
# against rows/23_built_from.sh; rows/22_uses_vault_policy.sh against
# rows/21). The family's edges are written by the
# SecurityGroupReachabilityMaterializationHandler -- the SAME handler that
# writes security_group_sg_rule's edges -- and a handler-stage kill of it is
# already claimed by rows/31_security_group_sg_rule.sh (first-landed
# tie-break). So this row does NOT reuse that handler-stage wait: it
# declares wait_stage=statement with the same
# security_group_reachability_materialization wait_key as rendezvous only
# (the claimed edge work item), while per-family exclusivity is judged on
# this row's own IFA_FAMILY_ANCHOR -- "a blocker that engages a write only
# it performs". The hold (hold-statement-before-delegation, matched on that
# anchor) lands post-retract, post-node-write, post-SG-edge-write, worker
# dead mid-handler: a fault point the sg_rule table_lock:fact_records kill
# (pre-write, at the fact load) never reaches.
#
# blocker_kind=statement_hold: the kill cell holds the family's own TO MERGE
# through the hold decorator (see IFA_FAMILY_ANCHOR below), not a lock.
# wait_stage=statement names the same security_group_reachability_materialization
# domain the handler-stage wait uses, but ONLY as the marker-plus-claim
# rendezvous -- handler-stage exclusivity still belongs to the sg_rule row
# by first-landed tie-break, and statement-stage exclusivity is judged on
# the anchor
# (go/internal/reducer/ifa_family_registry_statement_hold_coherence_test.go).
IFA_FAMILY_BLOCKER_KIND[security_group_rule_endpoint]="statement_hold"
IFA_FAMILY_WAIT_STAGE[security_group_rule_endpoint]="statement"
# The fact_work_items.domain the projector fans this family out under: the
# edge domain itself
# (DomainSecurityGroupReachabilityMaterialization in
# go/internal/reducer/contract/domain.go:146-147, fanned at
# go/internal/projector/security/group_reachability_intents.go:71-75).
# Same value as the sg_rule row's handler wait, deliberately: for a
# statement-stage row the key is rendezvous only.
IFA_FAMILY_WAIT_KEY[security_group_rule_endpoint]="security_group_reachability_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[security_group_rule_endpoint]=1

# ONE cassette serves the pair: the same shared reachability cassette both
# rows drive (sibling precedent: handles_route/runs_in/invokes_cloud_action
# share symbol_runtime_cassette across rows/09-11). TWO exact-set asserts:
# this row asserts the rule -> endpoint TO set.
IFA_FAMILY_DRIVE_FN[security_group_rule_endpoint]="ifa_security_group_reachability_drive"
IFA_FAMILY_ASSERT_FN[security_group_rule_endpoint]="ifa_security_group_rule_endpoint_assert"
IFA_FAMILY_CASSETTE_VAR[security_group_rule_endpoint]="security_group_reachability_cassette"
IFA_FAMILY_EXPECTED_VAR[security_group_rule_endpoint]="security_group_rule_endpoint_expected_edges"

# go/internal/storage/cypher/cloud/aws/security/group/security_reachability_edge_writer.go:101,
# which reads `MERGE (rule)-[rel:TO]->(target)` in source. The anchor is
# matched against EXECUTED statement text and the token is static in the
# template (no %s to fill -- the template's %s fills the target-node label),
# so the anchor carries the literal. Pinning a `%s` form here would match no
# executed statement and a scripted graph-write fault would never fire -- a
# green cell that tested nothing. The type is read off the template, never
# derived from the port or family name.
IFA_FAMILY_ANCHOR[security_group_rule_endpoint]="MERGE (rule)-[rel:TO]->(target)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[security_group_rule_endpoint]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[security_group_rule_endpoint]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[security_group_rule_endpoint]="0"

IFA_FAMILY_HANDLER_GO_FILE[security_group_rule_endpoint]="go/internal/reducer/secgroup/security_group_reachability_materialization.go"

IFA_FAMILY_NAMES+=(security_group_rule_endpoint)
