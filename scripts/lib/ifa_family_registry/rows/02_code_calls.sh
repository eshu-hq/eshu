#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# code_calls row. See ../../ifa_family_registry.sh for the schema and every
# array declaration this file assigns into.

# ifa_fault_generic_cells.sh's _ifa_generic_cell_killworker_body calls
# ifa_fault_start_shared_intent_lock for any family whose row declares this
# kind, and releases it in the same function. This family reaches that path through
# cell_killworker_family (cell_kind=generic below).
#
# The wrapper this row used to cite, ifa_code_call_start_intent_lock, was
# deleted with the migration -- it had no callers left, and a row whose proof
# points at an unexecuted helper is not proof.
IFA_FAMILY_BLOCKER_KIND[code_calls]="shared_intent_lock"
IFA_FAMILY_WAIT_STAGE[code_calls]="handler"
# Consumed by ifa_fault_generic_cells.sh's _ifa_generic_wait_for_claimed, which reads this row through
# ifa_family_wait_key and scopes ifa_fault_wait_for_claimed to it.
IFA_FAMILY_WAIT_KEY[code_calls]="code_call_materialization"
IFA_FAMILY_SHARED_CELL[code_calls]=1
# go/internal/storage/cypher/canonical_code_call_edges.go:70
# (batchCanonicalCodeCallUpsertCypher) is the live CALLS write template. Cited
# to the Cypher source rather than to a shell twin because there is no longer a
# twin to cite: code_call_edge_operation_match was deleted when this family
# moved to the generic cells, which read this field through ifa_family_anchor.
# canonical.go's single-row canonicalCodeCallUpsertCypher shares the same MERGE
# text but is commented as having no production caller, so it is not the source.
IFA_FAMILY_ANCHOR[code_calls]="MERGE (source)-[rel:CALLS]->(target)"
IFA_FAMILY_CELL_KIND[code_calls]="generic"

IFA_FAMILY_DRIVE_FN[code_calls]="ifa_code_call_drive"
IFA_FAMILY_ASSERT_FN[code_calls]="ifa_code_call_assert"
IFA_FAMILY_CASSETTE_VAR[code_calls]="code_call_cassette"
IFA_FAMILY_EXPECTED_VAR[code_calls]="code_call_expected_edges"

# ifa_fault_injection_cells.sh:76: baseline_code_call_retried is set by the
# shared cell_baseline
# (irregular: "code_call" singular, not the family name "code_calls").
IFA_FAMILY_RETRY_BASELINE_VAR[code_calls]="baseline_code_call_retried"

IFA_FAMILY_HANDLER_GO_FILE[code_calls]="go/internal/reducer/code_call_materialization.go"

# driven by drive_all_cassettes via ifa_code_call_drive (ifa_fault_injection_driver.sh:97).
IFA_FAMILY_FAULT_SHARED_DRIVE[code_calls]="1"

IFA_FAMILY_NAMES+=(code_calls)
