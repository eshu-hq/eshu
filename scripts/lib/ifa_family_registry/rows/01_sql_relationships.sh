#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# sql_relationships row. See ../../ifa_family_registry.sh for the schema and
# every array declaration this file assigns into (already declared empty by
# the time any row file is sourced).

# ifa_fault_injection_sql_cells.sh (cell_killworker_sql's own header): no lock
# is acquired before the kill. That header names exactly what this means it
# does NOT prove -- "What it does NOT prove: that the kill landed
# mid-handler", :78 -- and
# says the separate graph-write cell (cell_failgraphwrite_sql, anchored to
# the QUERIES_TABLE MERGE) is what actually backs the family's fault
# coverage claim. Recorded faithfully as none, not silently upgraded.
IFA_FAMILY_BLOCKER_KIND[sql_relationships]="none"
IFA_FAMILY_WAIT_STAGE[sql_relationships]="handler"
# ifa_fault_injection_sql_cells.sh:95: ifa_fault_wait_for_claimed(...,
# "sql_relationship_materialization") -- this family keeps its own hand-written
# call because its cells are custom.
IFA_FAMILY_WAIT_KEY[sql_relationships]="sql_relationship_materialization"
IFA_FAMILY_SHARED_CELL[sql_relationships]=1
# go/internal/storage/cypher/canonical.go:186 is the live QUERIES_TABLE write
# template. This family keeps a shell twin, sql_edge_operation_match
# (scripts/verify-ifa-fault-injection.sh:302), because its cells are still
# hand-written (cell_kind=custom); the two must stay byte-identical.
IFA_FAMILY_ANCHOR[sql_relationships]="MERGE (source)-[rel:QUERIES_TABLE]->(target)"
# custom: cell_killworker_sql and cell_failgraphwrite_sql are hand-written
# functions the gate dispatches by name, not through cell_killworker_family.
# blocker_kind=none IS a shape the generic dispatcher supports, which is why
# this row read generic before -- but cell_kind records how the family is
# dispatched today, not what the dispatcher could express.
IFA_FAMILY_CELL_KIND[sql_relationships]="custom"

# Irregular name: scripts/lib/ifa_sql_delta_live.sh, not the
# ifa_<family>_drive convention every other family follows.
IFA_FAMILY_DRIVE_FN[sql_relationships]="ifa_det_drive_sql_baseline"
IFA_FAMILY_ASSERT_FN[sql_relationships]="ifa_det_assert_sql_baseline"
IFA_FAMILY_CASSETTE_VAR[sql_relationships]="sql_cassette"
IFA_FAMILY_EXPECTED_VAR[sql_relationships]="sql_expected_edges"

IFA_FAMILY_NAMES+=(sql_relationships)
