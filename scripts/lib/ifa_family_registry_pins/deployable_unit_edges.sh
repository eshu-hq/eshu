#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# deployable_unit_edges hand-derived pin (#6147 PR-0 family registry).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh,
# which owns the hand-authored-literal rule, the totality diff, and the
# comparison against scripts/lib/ifa_family_registry.sh -- read that file's
# header before touching this one. Every value below is HAND-TYPED literal
# text, derived by reading the citations inline; it is never sourced,
# generated, or read back out of the registry.

# go/internal/reducer/deployable_unit_correlation.go:24 declares
# DeployableUnitCorrelationHandler; it has no IntentWriter field (only
# EdgeWriter, used by deployable_unit_correlation_edges.go:106,119 to write
# DomainDeployableUnitEdges directly). Handle() (intent.Domain check at
# deployable_unit_correlation.go:38 against DomainDeployableUnitCorrelation)
# also writes admission_decisions directly via
# deployable_unit_admission_decisions.go, a table only 3 handlers in the
# whole codebase write and only THIS one this gate drives (recorded in
# scripts/lib/ifa_fault_injection_deployable_unit_lock.sh's own header). The
# fault cell holds that table in ACCESS EXCLUSIVE MODE
# (ifa_deployable_unit_start_admission_decisions_lock:138, LOCK TABLE at :141,
# same file) => blocker_kind=table_lock:admission_decisions. Confirmed live:
# scripts/lib/ifa_fault_injection_deployable_unit_cells.sh:299 calls
# ifa_fault_wait_for_claimed against fact_work_items domain
# "deployable_unit_correlation" (go/internal/reducer/intent.go:18
# DomainDeployableUnitCorrelation Domain = "deployable_unit_correlation") --
# handler stage, wait_key="deployable_unit_correlation" (the reducer-intent
# domain the correlation handler claims under, NOT the
# "deployable_unit_edges" shared-projection/graph-domain name the family is
# registered under).
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:admission_decisions"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="deployable_unit_correlation"

# go/internal/storage/cypher/canonical_deployable_unit_edges.go:6-9
# (batchCanonicalDeployableUnitCorrelationUpsertCypher) is the family's only
# CORRELATES_DEPLOYABLE_UNIT write template (matches
# scripts/verify-ifa-fault-injection.sh:318
# deployable_unit_edge_operation_match). shared_cell: NOT driven by
# drive_all_cassettes -- scripts/lib/ifa_fault_injection_driver.sh:111-119's
# own header states drive_deployable_unit_cassette is called only by this
# family's three dedicated cells, "never unconditionally by every cell", and
# on the determinism gate ifa_family_fixtures.sh:56-63 / the standalone-cell
# ordering guard in scripts/lib/test-ifa-determinism-family-cases.sh:91-99
# both independently confirm it runs in its own cell after the N-loop
# closes, not inside it (a bootstrap-index maintenance pass this family
# alone needs) => shared_cell=0. cell_kind: blocker_kind=table_lock:
# admission_decisions IS one of the generic dispatcher's supported shapes
# (scripts/lib/ifa_fault_generic_cells.sh's `cell_killworker_family` case
# statement's `table_lock:*)` arm, :330) -- but the live driver has NOT been
# migrated to dispatch this family through it:
# scripts/verify-ifa-fault-injection.sh:471-472 still calls
# cell_killworker_deployable_unit / cell_failgraphwrite_deployable_unit
# directly (its own hand-written, already-proven cell functions in
# ifa_fault_injection_deployable_unit_cells.sh), not
# cell_killworker_family/cell_failgraphwrite_family. cell_kind=custom
# describes the actual, current dispatch reality for this family, driven by
# a deliberate migration-scoping decision, not an architectural constraint
# -- checked by reading the live driver's call sites, not assumed from
# blocker_kind alone.
IFA_FAMILY_PIN_ANCHOR="MERGE (source_repo)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(deployment_repo)"
IFA_FAMILY_PIN_SHARED_CELL=0
IFA_FAMILY_PIN_CELL_KIND="custom"
