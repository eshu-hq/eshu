#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# workload_cloud_relationship row (#6228). See ../../ifa_family_registry.sh for
# the schema and every array declaration this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are unavailable to
# one. This row carries NO fault cells yet: the determinism gate drives it
# through the shared N={1,2,4} cell below, and the fault-injection area stays
# untouched (cell_kind=custom is rejected by the generic dispatchers, and no
# custom cell file exists, so the fault gate never dispatches this family).

# Hand-derived, and non-vacuous once the determinism gate drives it:
# WorkloadCloudRelationshipMaterializationHandler embeds `FactLoader
# FactLoader`
# (go/internal/reducer/workload_cloud_relationship_materialization.go:58)
# and Handle refuses to run without it (:103) before passing it to
# loadFactsForKinds (:138). The handler reads fact_records AFTER claiming its
# work item, so an ACCESS EXCLUSIVE lock on that table would hold it genuinely
# in flight -- the blocker_kind a future fault cell would take. Same
# table and same reasoning as rows/13; see that row for why fact_work_items is
# the wrong target.
IFA_FAMILY_BLOCKER_KIND[workload_cloud_relationship]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[workload_cloud_relationship]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming the same domain (:236), and corroborated by the domain definition
# (DomainWorkloadCloudRelationshipMaterialization in
# go/internal/reducer/contract/domain.go:155). Proven on
# a live stack by this row's own determinism drive, which leaves exactly one
# `workload_cloud_relationship_materialization | succeeded` row per cell.
IFA_FAMILY_WAIT_KEY[workload_cloud_relationship]="workload_cloud_relationship_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[workload_cloud_relationship]=1

IFA_FAMILY_DRIVE_FN[workload_cloud_relationship]="ifa_workload_cloud_relationship_drive"
IFA_FAMILY_ASSERT_FN[workload_cloud_relationship]="ifa_workload_cloud_relationship_assert"
IFA_FAMILY_CASSETTE_VAR[workload_cloud_relationship]="workload_cloud_relationship_cassette"
IFA_FAMILY_EXPECTED_VAR[workload_cloud_relationship]="workload_cloud_relationship_expected_edges"

# go/internal/storage/cypher/workload_cloud_relationship_writer.go:23, which
# reads `MERGE (instance)-[rel:%s]->(resource)` in source. The anchor is
# matched against EXECUTED statement text; the %s is filled from the closed
# single-member workloadCloudRelationshipVocabulary (:15-17, `"USES": {}`),
# screened per row by validateWorkloadCloudRelationshipType (:143, enforced at
# :79), so the executed text carries the literal
# `MERGE (instance)-[rel:USES]->(resource)`. Pinning a `%s` form here would
# match no executed statement and a scripted graph-write fault would never
# fire -- a green cell that tested nothing.
#
# It is NOT WORKLOAD_USES_CLOUD_RESOURCE: that string is the
# workloadCloudRelationshipEdgeLabel const (:13), statement metadata carried
# beside the query that never reaches the graph.
IFA_FAMILY_ANCHOR[workload_cloud_relationship]="MERGE (instance)-[rel:USES]->(resource)"
# custom, prospectively: no fault cell file exists for this family yet, and the
# generic public dispatchers reject `cell_kind=custom`, so the fault gate
# cannot dispatch it by accident. Flip only with a proven fault cell, never to
# generic without the lifecycle the generic header demands.
IFA_FAMILY_CELL_KIND[workload_cloud_relationship]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[workload_cloud_relationship]=""

# 0: drive_all_cassettes does not produce this family, and no fault cell
# drives it either; each determinism cell drives it through
# DRIVE_FN/CASSETTE_VAR instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[workload_cloud_relationship]="0"

IFA_FAMILY_HANDLER_GO_FILE[workload_cloud_relationship]="go/internal/reducer/workload_cloud_relationship_materialization.go"

IFA_FAMILY_NAMES+=(workload_cloud_relationship)
