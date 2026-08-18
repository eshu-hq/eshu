#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# code_calls row. See ../../ifa_family_registry.sh for the schema and every
# array declaration this file assigns into.

# ifa_fault_injection_code_call_cells.sh:26-29: ifa_code_call_start_intent_lock
# calls ifa_fault_start_shared_intent_lock directly.
IFA_FAMILY_BLOCKER_KIND[code_calls]="shared_intent_lock"
IFA_FAMILY_WAIT_STAGE[code_calls]="handler"
# ifa_fault_injection_code_call_cells.sh:53: (..., "code_call_materialization").
IFA_FAMILY_WAIT_KEY[code_calls]="code_call_materialization"
IFA_FAMILY_SHARED_CELL[code_calls]=1
# scripts/verify-ifa-fault-injection.sh:280
IFA_FAMILY_ANCHOR[code_calls]="MERGE (source)-[rel:CALLS]->(target)"
IFA_FAMILY_CELL_KIND[code_calls]="generic"

IFA_FAMILY_DRIVE_FN[code_calls]="ifa_code_call_drive"
IFA_FAMILY_ASSERT_FN[code_calls]="ifa_code_call_assert"
IFA_FAMILY_CASSETTE_VAR[code_calls]="code_call_cassette"
IFA_FAMILY_EXPECTED_VAR[code_calls]="code_call_expected_edges"

# ifa_fault_injection_code_call_cells.sh:65: baseline_code_call_retried
# (irregular: "code_call" singular, not the family name "code_calls").
IFA_FAMILY_RETRY_BASELINE_VAR[code_calls]="baseline_code_call_retried"

IFA_FAMILY_HANDLER_GO_FILE[code_calls]="go/internal/reducer/code_call_materialization.go"

IFA_FAMILY_NAMES+=(code_calls)
