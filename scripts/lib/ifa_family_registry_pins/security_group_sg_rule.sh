#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# security_group_sg_rule hand-derived pin (#6228 secgroup-pair live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# go/internal/reducer/secgroup/security_group_reachability_materialization.go
# declares `FactLoader factload.FactLoader` as a struct field at :121,
# Handle rejects a nil one at :155-157, and passes it to its fact load at
# :187-193. The loader is factload.LoadFactsForKinds
# (go/internal/reducer/factload/loader.go:55-67), which pages over
# FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,103,130). An ACCESS
# EXCLUSIVE lock on fact_records therefore blocks a read this handler
# really performs -- the blocker_kind this family's fault cell takes.
#
# FIRST-LANDED of the pair: both families' edges are written by the ONE
# SecurityGroupReachabilityMaterializationHandler, and this family owns the
# handler-stage wait by first-landed tie-break (sibling precedent:
# rows/23_built_from.sh owns container_image_identity; rows/26 takes the
# per-statement seam). The sibling security_group_rule_endpoint row takes
# statement_hold on the same wait key as rendezvous only.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from the projector intent
# (go/internal/projector/security/group_reachability_intents.go:71-75 fans
# DomainSecurityGroupReachabilityMaterialization),
# and corroborated by the domain definition
# (DomainSecurityGroupReachabilityMaterialization in
# go/internal/reducer/contract/domain.go:146-147).
IFA_FAMILY_PIN_WAIT_KEY="security_group_reachability_materialization"

# go/internal/storage/cypher/cloud/aws/security/group/security_reachability_edge_writer.go:87,
# which reads `MERGE (sg)-[rel:%s]->(rule)` in source. The anchor is
# matched against EXECUTED statement text as a SUBSTRING, so it carries the
# PREFIX form: the %s is substituted per row from the closed two-member
# direction vocabulary (ALLOWS_INGRESS/ALLOWS_EGRESS), so no single filled
# spelling matches every execution -- the observability_coverage rationale
# transfers (its `MERGE (obs)-[rel:AWS_COVERS_` prefix covers a five-member
# vocabulary the same way). A filled `MERGE (sg)-[rel:ALLOWS_INGRESS]->(rule)`
# spelling would fault only the ingress half and leave the egress screening
# unproven. No other production statement MERGEs `(sg)-[rel:` -- the rule
# node upsert MERGEs `(r:SecurityGroupRule`, the TO template MERGEs
# `(rule)-[rel:TO]`, the CIDR/prefix node writer MERGEs `(c:CidrBlock` /
# `(p:PrefixList`.
IFA_FAMILY_PIN_ANCHOR="MERGE (sg)-[rel:ALLOWS_"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_security_group_sg_rule_cells.sh,
# dispatched by name (#6228 secgroup-pair live leaf). Custom rather
# than generic for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
