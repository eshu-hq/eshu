#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# rationale_edges row. See ../../ifa_family_registry.sh for the schema and
# every array declaration this file assigns into.

# ifa_fault_injection_rationale_cells.sh:19: cell_killworker_rationale calls
# ifa_fault_start_shared_intent_lock directly.
IFA_FAMILY_BLOCKER_KIND[rationale_edges]="shared_intent_lock"
IFA_FAMILY_WAIT_STAGE[rationale_edges]="handler"
# ifa_fault_injection_rationale_cells.sh:23: (..., "rationale_materialization").
IFA_FAMILY_WAIT_KEY[rationale_edges]="rationale_materialization"
IFA_FAMILY_SHARED_CELL[rationale_edges]=1
# scripts/lib/test-ifa-fault-injection-rationale-cases.sh:359 (pins the live
# rationale_edge_operation_match value byte-for-byte)
IFA_FAMILY_ANCHOR[rationale_edges]="MERGE (rationale)-[rel:EXPLAINS]->(target)"
IFA_FAMILY_CELL_KIND[rationale_edges]="generic"

IFA_FAMILY_DRIVE_FN[rationale_edges]="ifa_rationale_drive"
IFA_FAMILY_ASSERT_FN[rationale_edges]="ifa_rationale_assert"
IFA_FAMILY_CASSETTE_VAR[rationale_edges]="rationale_cassette"
IFA_FAMILY_EXPECTED_VAR[rationale_edges]="rationale_expected_edges"

# ifa_fault_injection_rationale_cells.sh:33: baseline_rationale_retried.
IFA_FAMILY_RETRY_BASELINE_VAR[rationale_edges]="baseline_rationale_retried"

IFA_FAMILY_HANDLER_GO_FILE[rationale_edges]="go/internal/reducer/rationale_edge_materialization.go"

IFA_FAMILY_NAMES+=(rationale_edges)
