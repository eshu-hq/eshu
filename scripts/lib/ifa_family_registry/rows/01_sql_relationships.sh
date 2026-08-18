#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# sql_relationships row. See ../../ifa_family_registry.sh for the schema and
# every array declaration this file assigns into (already declared empty by
# the time any row file is sourced).

# ifa_fault_injection_sql_cells.sh:44-63 (cell_killworker_sql's own header):
# no lock is acquired before the kill. That header names exactly what this
# means it does NOT prove -- "that the kill landed mid-handler" -- and
# says the separate graph-write cell (cell_failgraphwrite_sql, anchored to
# the QUERIES_TABLE MERGE) is what actually backs the family's fault
# coverage claim. Recorded faithfully as none, not silently upgraded.
IFA_FAMILY_BLOCKER_KIND[sql_relationships]="none"
IFA_FAMILY_WAIT_STAGE[sql_relationships]="handler"
# ifa_fault_injection_sql_cells.sh:73: ifa_fault_wait_for_claimed(...,
# "sql_relationship_materialization").
IFA_FAMILY_WAIT_KEY[sql_relationships]="sql_relationship_materialization"
IFA_FAMILY_SHARED_CELL[sql_relationships]=1
# scripts/verify-ifa-fault-injection.sh:279
IFA_FAMILY_ANCHOR[sql_relationships]="MERGE (source)-[rel:QUERIES_TABLE]->(target)"
IFA_FAMILY_CELL_KIND[sql_relationships]="generic"

# Irregular name: scripts/lib/ifa_sql_delta_live.sh, not the
# ifa_<family>_drive convention every other family follows.
IFA_FAMILY_DRIVE_FN[sql_relationships]="ifa_det_drive_sql_baseline"
IFA_FAMILY_ASSERT_FN[sql_relationships]="ifa_det_assert_sql_baseline"
IFA_FAMILY_CASSETTE_VAR[sql_relationships]="sql_cassette"
IFA_FAMILY_EXPECTED_VAR[sql_relationships]="sql_expected_edges"

IFA_FAMILY_NAMES+=(sql_relationships)
