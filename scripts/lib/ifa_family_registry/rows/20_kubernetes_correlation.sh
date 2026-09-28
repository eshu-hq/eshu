#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# kubernetes_correlation row (#6228). See ../../ifa_family_registry.sh for
# the schema and every array declaration this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are unavailable to
# one. This row carries NO fault cells yet: the determinism gate drives it
# through the shared N={1,2,4} cell below, and the fault-injection area stays
# untouched (cell_kind=custom is rejected by the generic dispatchers, and no
# custom cell file exists, so the fault gate never dispatches this family).

# Hand-derived, and non-vacuous once the determinism gate drives it:
# KubernetesCorrelationMaterializationHandler embeds `FactLoader
# factload.FactLoader`
# (go/internal/reducer/kubernetescorrelation/kubernetes_correlation_materialization.go:99)
# and Handle refuses to run without it (:124) before passing it to
# factload.LoadFactsForKinds (:250). The handler reads fact_records AFTER
# claiming its work item, so an ACCESS EXCLUSIVE lock on that table would hold
# it genuinely in flight -- the blocker_kind a future fault cell would take.
# Same table and same reasoning as rows/13; see that row for why
# fact_work_items is the wrong target.
IFA_FAMILY_BLOCKER_KIND[kubernetes_correlation]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[kubernetes_correlation]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from go/internal/storage/postgres/reducer_queue_readiness_sql.go's readiness
# row naming the same domain (:248), and corroborated by the domain
# definition (KubernetesCorrelationMaterializationDomainDefinition in
# go/internal/reducer/kubernetescorrelation/kubernetes_correlation_materialization.go:35).
# Proven on a live stack by this row's own determinism drive, which leaves
# exactly one `kubernetes_correlation_materialization | succeeded` row per cell.
IFA_FAMILY_WAIT_KEY[kubernetes_correlation]="kubernetes_correlation_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[kubernetes_correlation]=1

IFA_FAMILY_DRIVE_FN[kubernetes_correlation]="ifa_kubernetes_correlation_drive"
IFA_FAMILY_ASSERT_FN[kubernetes_correlation]="ifa_kubernetes_correlation_assert"
IFA_FAMILY_CASSETTE_VAR[kubernetes_correlation]="kubernetes_correlation_cassette"
IFA_FAMILY_EXPECTED_VAR[kubernetes_correlation]="kubernetes_correlation_expected_edges"

# go/internal/storage/cypher/kubernetes_correlation_edge_writer.go:51, which
# reads `MERGE (w)-[rel:RUNS_IMAGE]->(img)` in source. The anchor is
# matched against EXECUTED statement text, so it carries the filled form:
# RUNS_IMAGE is a static token in
# CanonicalKubernetesCorrelationEdgeUpsertCypherFormat, kept out of the
# MERGE property map, so no row content can change the executed literal --
# while the template's one %s (the source-node label) IS substituted per
# row from kubernetesEdgeSourceLabelVocabulary and never appears in the
# MERGE clause. Pinning a `%s` form here would match no executed
# statement and a scripted graph-write fault would never fire -- a green
# cell that tested nothing.
#
# It is NOT KUBERNETES_CORRELATION: that string is statement metadata
# carried beside the query that never reaches the graph. The type is read
# off the template, never derived from the port or family name.
IFA_FAMILY_ANCHOR[kubernetes_correlation]="MERGE (w)-[rel:RUNS_IMAGE]->(img)"
# custom, prospectively: no fault cell file exists for this family yet, and the
# generic public dispatchers reject `cell_kind=custom`, so the fault gate
# cannot dispatch it by accident. Flip only with a proven fault cell, never to
# generic without the lifecycle the generic header demands.
IFA_FAMILY_CELL_KIND[kubernetes_correlation]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[kubernetes_correlation]=""

# 0: drive_all_cassettes does not produce this family, and no fault cell
# drives it either; each determinism cell drives it through
# DRIVE_FN/CASSETTE_VAR instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[kubernetes_correlation]="0"

IFA_FAMILY_HANDLER_GO_FILE[kubernetes_correlation]="go/internal/reducer/kubernetescorrelation/kubernetes_correlation_materialization.go"

IFA_FAMILY_NAMES+=(kubernetes_correlation)
