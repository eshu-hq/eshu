#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# deployable_unit_edges row. See ../../ifa_family_registry.sh for the schema
# and every array declaration this file assigns into.

# ifa_fault_injection_deployable_unit_lock.sh:76-137: after two wrong lock
# targets (both documented in that file, both confirmed-in-CI failures -- a
# vacuous shared_projection_intents lock, then a starvation-inducing
# graph_projection_phase_state lock), admission_decisions is the correct and
# only-safe target for this family.
IFA_FAMILY_BLOCKER_KIND[deployable_unit_edges]="table_lock:admission_decisions"
IFA_FAMILY_WAIT_STAGE[deployable_unit_edges]="handler"
# ifa_fault_injection_deployable_unit_lock.sh's own comment block (:90-96)
# names the domain its wait_for_claimed precondition scopes to.
IFA_FAMILY_WAIT_KEY[deployable_unit_edges]="deployable_unit_correlation"
# #5993: its own standalone cell after the N-loop, never folded into it
# (ifa_deployable_unit_live_run_standalone_cell's own header explains why --
# a bootstrap-index maintenance pass this family alone needs would move
# every OTHER family's digest terminal for an unrelated reason).
IFA_FAMILY_SHARED_CELL[deployable_unit_edges]=0
# Pinned by the deployable-unit MERGE operation_match anchor in
# scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh.
IFA_FAMILY_ANCHOR[deployable_unit_edges]="MERGE (source_repo)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(deployment_repo)"

# SCOPING CALL: this family's blocker_kind (table_lock:admission_decisions)
# IS one of the two shapes ifa_fault_generic_cells.sh supports, and generic
# table_lock support is built and unit-tested. It is registered custom
# anyway for THIS PR: the generic path's two real, migrated consumers are
# code_calls and rationale_edges (both shared_intent_lock) only, so this
# family keeps its bespoke, already-proven cell_killworker_deployable_unit /
# cell_failgraphwrite_deployable_unit
# (ifa_fault_injection_deployable_unit_cells.sh) rather than migrating onto
# an unexercised generic mechanism. Flip this to generic in the same change
# that migrates its cell functions, not before.
IFA_FAMILY_CELL_KIND[deployable_unit_edges]="custom"

# Not a shared_cell family (its own standalone determinism cell), but the
# generic fault cells (ifa_fault_generic_cells.sh) need this too. Note this is
# not the only table_lock:<name> family -- codeowners_ownership_edges declares
# table_lock:fact_records (rows/06) -- and neither reaches the generic
# dispatcher, since both are cell_kind=custom.
IFA_FAMILY_CASSETTE_VAR[deployable_unit_edges]="deployable_unit_cassette"
IFA_FAMILY_EXPECTED_VAR[deployable_unit_edges]="deployable_unit_expected_edges"


# NOT in drive_all_cassettes: drive_deployable_unit_cassette (ifa_fault_injection_driver.sh:120) is called only by this family's own three cells.
IFA_FAMILY_FAULT_SHARED_DRIVE[deployable_unit_edges]="0"

IFA_FAMILY_NAMES+=(deployable_unit_edges)
