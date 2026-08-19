#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# rationale_edges row. See ../../ifa_family_registry.sh for the schema and
# every array declaration this file assigns into.

# ifa_fault_generic_cells.sh:184: the generic kill cell calls
# ifa_fault_start_shared_intent_lock for any family whose row declares this
# kind, and :199 releases it. cell_killworker_rationale is now a one-line
# delegation to cell_killworker_family, so the lock is taken there, not in this
# familys own cells lib.
IFA_FAMILY_BLOCKER_KIND[rationale_edges]="shared_intent_lock"
IFA_FAMILY_WAIT_STAGE[rationale_edges]="handler"
# Consumed by ifa_fault_generic_cells.sh:145, which reads this row through
# ifa_family_wait_key and scopes ifa_fault_wait_for_claimed to it.
IFA_FAMILY_WAIT_KEY[rationale_edges]="rationale_materialization"
IFA_FAMILY_SHARED_CELL[rationale_edges]=1
# go/internal/storage/cypher/canonical_rationale_edges.go:41 is the live
# EXPLAINS write template. Cited to the Cypher source rather than to a shell
# twin because there is no longer a twin to cite: rationale_edge_operation_match
# was deleted when this family moved to the generic cells, which read this field
# through ifa_family_anchor.
IFA_FAMILY_ANCHOR[rationale_edges]="MERGE (rationale)-[rel:EXPLAINS]->(target)"
IFA_FAMILY_CELL_KIND[rationale_edges]="generic"

IFA_FAMILY_DRIVE_FN[rationale_edges]="ifa_rationale_drive"
IFA_FAMILY_ASSERT_FN[rationale_edges]="ifa_rationale_assert"
IFA_FAMILY_CASSETTE_VAR[rationale_edges]="rationale_cassette"
IFA_FAMILY_EXPECTED_VAR[rationale_edges]="rationale_expected_edges"

# ifa_fault_injection_cells.sh:82: baseline_rationale_retried is set by the
# shared cell_baseline.
IFA_FAMILY_RETRY_BASELINE_VAR[rationale_edges]="baseline_rationale_retried"

IFA_FAMILY_HANDLER_GO_FILE[rationale_edges]="go/internal/reducer/rationale_edge_materialization.go"

IFA_FAMILY_NAMES+=(rationale_edges)
