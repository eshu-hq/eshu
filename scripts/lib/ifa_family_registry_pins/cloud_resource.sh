#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# cloud_resource hand-derived pin (#8358). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that
# file's header before touching this one. Every value is HAND-TYPED,
# derived from the citations inline, never read back out of the registry
# row.

# go/internal/reducer/aws_relationship_materialization.go declares
# `FactLoader FactLoader` as a struct field (:88), Handle rejects a nil
# one (:118), and loads the scope generation's facts through it before any
# graph write. An ACCESS EXCLUSIVE lock on fact_records therefore blocks a
# read this handler really performs.
#
# A DIRECT-materialization family, so there is no
# shared_projection_intents row for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the targeted AWS handler runs under, read off
# go/internal/storage/postgres/reducer_queue_readiness_sql.go (its
# readiness row names aws_relationship_materialization against
# canonical_nodes_committed/cloud_resource_uid) and corroborated by the
# family's live baseline drain. The GCP/Azure handlers run under
# gcp_relationship_materialization / azure_relationship_materialization;
# the row targets the AWS half deliberately (see rows/21's header).
IFA_FAMILY_PIN_WAIT_KEY="aws_relationship_materialization"

# canonicalCloudResourceEdgeUpsertCypherFormat in
# go/internal/storage/cypher/cloud/resource/edge_writer.go reads
# `MERGE (source)-[rel:%s]->(target)`. The anchor is matched against
# EXECUTED statement text, so the type is pinned INTERPOLATED for the one
# verb the family cassette drives first: the %s is
# AWSRelationshipCypherTypePrefix ("AWS_") + lambda_function_uses_execution_role.
# A literal `%s` here would match no executed statement, the scripted
# graph-write fault would never fire, and the cell would report green
# having tested nothing.
#
# NOT AWS_RELATIONSHIP -- that is the writer's statement-metadata label
# (StatementMetadataEntityLabelKey), never a graph relationship type.
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:AWS_lambda_function_uses_execution_role]->(target)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_cloud_resource_cells.sh, dispatched by
# name. Custom rather than generic for the reason recorded in the sibling
# kubernetes_namespace_environment pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
