#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# cloud_resource row (#8358). See ../../ifa_family_registry.sh for the schema
# and every array declaration this file assigns into. Read
# rows/12_kubernetes_namespace_environment.sh's header first -- it explains
# what a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FIRST PREDICATE-SCOPED FAMILY IN THIS REGISTRY. Every other row owns a
# finite relationship-type set; this family's three writers (AWS, GCP,
# Azure, behind the one WriteCloudResourceEdges port) prefix an open
# per-provider verb vocabulary, so `eshu-ifa assert-edges -domain
# cloud_resource` scopes by the registered predicate (type prefix +
# evidence_source + CloudResource endpoints) in
# go/internal/storage/cypher/edge/materialized/predicate_families.go instead
# of a type list. That changes nothing in this row's shape -- the gate
# drives a cassette and asserts an exact edge set either way.
#
# THREE HANDLERS, ONE ROW. The cassette carries one scope generation per
# provider, fanned out under aws_relationship_materialization,
# gcp_relationship_materialization, and azure_relationship_materialization.
# The wait/retry machinery below names ONE domain, so the fault cells target
# the AWS handler (the family's first writer) and assert the FULL four-edge
# set on recovery: convergence of the untouched GCP/Azure edges is part of
# what the assert proves, not a gap in it.

# Hand-derived, and non-vacuous once a fault cell exists:
# AWSRelationshipMaterializationHandler embeds `FactLoader FactLoader`
# (go/internal/reducer/aws_relationship_materialization.go:88) and Handle
# refuses to run without it (:118) before loading its facts through it. The
# handler reads fact_records AFTER claiming its work item, so an ACCESS
# EXCLUSIVE lock on that table holds it genuinely in flight. Same table and
# same reasoning as rows/12; see that row for why fact_work_items is the
# wrong target.
IFA_FAMILY_BLOCKER_KIND[cloud_resource]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[cloud_resource]="handler"
# The fact_work_items.domain the targeted (AWS) handler runs under, read off
# the readiness table (go/internal/storage/postgres/
# reducer_queue_readiness_sql.go names aws_relationship_materialization
# against canonical_nodes_committed/cloud_resource_uid) and corroborated by
# the family's live baseline drain.
IFA_FAMILY_WAIT_KEY[cloud_resource]="aws_relationship_materialization"
# A plain reducer family needing no maintenance pass, so it is driven
# uniformly in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[cloud_resource]=1

IFA_FAMILY_DRIVE_FN[cloud_resource]="ifa_cloud_resource_drive"
IFA_FAMILY_ASSERT_FN[cloud_resource]="ifa_cloud_resource_assert"
IFA_FAMILY_CASSETTE_VAR[cloud_resource]="cloud_resource_cassette"
IFA_FAMILY_EXPECTED_VAR[cloud_resource]="cloud_resource_expected_edges"

# go/internal/storage/cypher/cloud_resource_edge_writer.go's
# canonicalCloudResourceEdgeUpsertCypherFormat reads
# `MERGE (source)-[rel:%s]->(target)`. The anchor is matched against
# EXECUTED statement text, so it carries an INTERPOLATED type: the %s is
# filled per row from AWSRelationshipCypherTypePrefix + the collector verb,
# an OPEN set, so the anchor pins the one verb this family's cassette
# drives first (lambda_function_uses_execution_role). Pinning the literal
# `%s` would match no executed statement and the scripted fault would never
# fire. It is NOT AWS_RELATIONSHIP, the writer's statement-metadata label.
IFA_FAMILY_ANCHOR[cloud_resource]="MERGE (source)-[rel:AWS_lambda_function_uses_execution_role]->(target)"
# custom, same dispatch shape as rows/12: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[cloud_resource]="custom"

# Not a shared_intent_lock family, so no retry baseline is required.
# Declared empty rather than omitted -- see rows/12 for why an absent key
# is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[cloud_resource]=""

# 0 on the same footing as rows/12: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[cloud_resource]="0"

# The targeted handler (see the header): the GCP and Azure handler files
# are gcp_relationship_materialization.go / azure_relationship_materialization.go
# in the same directory, same FactLoader shape.
IFA_FAMILY_HANDLER_GO_FILE[cloud_resource]="go/internal/reducer/aws_relationship_materialization.go"

IFA_FAMILY_NAMES+=(cloud_resource)
