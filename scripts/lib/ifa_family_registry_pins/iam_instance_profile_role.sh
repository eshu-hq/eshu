#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# iam_instance_profile_role hand-derived pin (#6228). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/iam_instance_profile_role_materialization.go declares
# `FactLoader FactLoader` as a struct field at :57, Handle rejects a nil one at
# :85, and passes it into the extraction path at :114. An ACCESS EXCLUSIVE lock
# on fact_records therefore blocks a read this handler really performs.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from a live drain of the committed cassette (one row,
# `iam_instance_profile_role_materialization | succeeded`) and corroborated by
# go/internal/storage/postgres/reducer_queue_readiness_sql.go:151, whose
# readiness row names the same domain.
IFA_FAMILY_PIN_WAIT_KEY="iam_instance_profile_role_materialization"

# go/internal/storage/cypher/iam_instance_profile_role_edge_writer.go:22 reads
# `MERGE (profile)-[rel:%s]->(role)`. The anchor is matched against EXECUTED
# statement text, so the type is pinned INTERPOLATED: the %s is filled from
# iamInstanceProfileRoleRelationshipVocabulary, a closed single-member set
# screened per row, so HAS_ROLE is the only value it takes. A literal `%s` here
# would match no executed statement, the scripted graph-write fault would never
# fire, and the cell would report green having tested nothing.
#
# NOT IAM_INSTANCE_PROFILE_HAS_ROLE -- that is iamInstanceProfileRoleEdgeLabel,
# statement metadata beside the query, never a graph relationship type.
IFA_FAMILY_PIN_ANCHOR="MERGE (profile)-[rel:HAS_ROLE]->(role)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh,
# dispatched by name (#6309). Custom rather than generic for the reason
# recorded in the sibling kubernetes_namespace_environment pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
