#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# security_group_rule_endpoint hand-derived pin (#6228 secgroup-pair live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# PER-STATEMENT HOLD (sibling precedent: rows/26_derived_from.sh against
# rows/23_built_from.sh; rows/22_uses_vault_policy.sh against rows/21).
# The family's edges are written by the
# SecurityGroupReachabilityMaterializationHandler -- the SAME handler that
# writes security_group_sg_rule's edges -- and a handler-stage kill of it is
# already claimed by the sg_rule row (first-landed tie-break). So this row
# does NOT reuse that handler-stage wait: it declares wait_stage=statement
# with the same security_group_reachability_materialization wait_key as
# rendezvous only (the claimed edge work item), while per-family exclusivity
# is judged on this row's own IFA_FAMILY_ANCHOR -- "a blocker that engages a
# write only it performs". The hold (hold-statement-before-delegation,
# matched on that anchor) lands post-retract, post-node-write,
# post-SG-edge-write, worker dead mid-handler: a fault point the sg_rule
# table_lock:fact_records kill (pre-write, at the fact load) never reaches.
#
# blocker_kind=statement_hold: the kill cell holds the family's own TO MERGE
# through the hold decorator (see IFA_FAMILY_ANCHOR below), not a lock.
# wait_stage=statement names the same security_group_reachability_materialization
# domain the handler-stage wait uses, but ONLY as the marker-plus-claim
# rendezvous -- handler-stage exclusivity still belongs to the sg_rule row
# by first-landed tie-break, and statement-stage exclusivity is judged on
# the anchor.
IFA_FAMILY_PIN_BLOCKER_KIND="statement_hold"
IFA_FAMILY_PIN_WAIT_STAGE="statement"
# The fact_work_items.domain the projector fans this family out under: the
# edge domain itself
# (DomainSecurityGroupReachabilityMaterialization in
# go/internal/reducer/contract/domain.go:146-147, fanned at
# go/internal/projector/security/group_reachability_intents.go:71-75).
# Same value as the sg_rule row's handler wait, deliberately: for a
# statement-stage row the key is rendezvous only.
IFA_FAMILY_PIN_WAIT_KEY="security_group_reachability_materialization"

# go/internal/storage/cypher/cloud/aws/security/group/security_reachability_edge_writer.go:101,
# which reads `MERGE (rule)-[rel:TO]->(target)` in source. The anchor is
# matched against EXECUTED statement text and the token is static in the
# template (no %s to fill -- the %s in the TO template fills the target-node
# LABEL, not the relationship type), so the anchor carries the literal.
# Pinning a `%s` form here would match no executed statement and a scripted
# graph-write fault would never fire -- a green cell that tested nothing.
# It is NOT a bare TO fragment: the full MERGE literal pins the exact
# statement (rule endpoint to endpoint node). The type is read off the
# template, never derived from the port or family name.
IFA_FAMILY_PIN_ANCHOR="MERGE (rule)-[rel:TO]->(target)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_security_group_rule_endpoint_cells.sh,
# dispatched by name (#6228 secgroup-pair live leaf). Custom rather
# than generic for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
