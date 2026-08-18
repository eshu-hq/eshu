#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# documentation_edges hand-derived pin (#6147 PR-0 family registry). Sourced
# by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh, which owns
# the hand-authored-literal rule, the totality diff, and the comparison
# against scripts/lib/ifa_family_registry.sh -- read that file's header
# before touching this one. Every value below is HAND-TYPED literal text,
# derived by reading the citations inline; it is never sourced, generated,
# or read back out of the registry.

# go/internal/reducer/documentation_edge_materialization.go:23-33 declares
# DocumentationEdgeMaterializationHandler with FactLoader, EdgeWriter, and
# PriorGenerationCheck fields ONLY -- no IntentWriter field anywhere in the
# struct or the file (`rg -c IntentWriter
# go/internal/reducer/documentation_edge_materialization.go` returns nothing).
# Handle() calls only h.EdgeWriter.RetractEdges/WriteEdges (lines 78, 90); it
# never touches shared_projection_intents. A shared_intent_lock on that table
# would therefore never engage for this family -- confirmed by this suite's
# own history: scripts/lib/test-ifa-fault-injection-documentation-cases.sh:302-307
# asserts ifa_documentation_start_intent_lock/ifa_documentation_release_intent_lock/
# "LOCK TABLE shared_projection_intents" must NOT survive in
# ifa_fault_injection_documentation_cells.sh, because that lock was vacuous
# for this family. The family's real blocker, landed under #5998, is a
# BEFORE UPDATE trigger on public.fact_work_items
# (scripts/lib/ifa_fault_injection_documentation_ack_setup.sh, asserted at
# scripts/lib/test-ifa-fault-injection-documentation-cases.sh:51,57-65) that
# blocks the row's own claimed->succeeded ACK transition
# (NEW.status = 'succeeded', OLD.stage = 'reducer',
# NEW.domain = 'documentation_materialization') => blocker_kind=ack_barrier,
# wait_stage=handler (it blocks the family's own fact_work_items row, not a
# separate runner's queue), wait_key="documentation_materialization"
# (go/internal/reducer/intent.go:66 DomainDocumentationMaterialization
# Domain = "documentation_materialization").
IFA_FAMILY_PIN_BLOCKER_KIND="ack_barrier"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="documentation_materialization"

# go/internal/storage/cypher/canonical_documentation_edges.go:17-32 (entity
# target) and :34-49 (workload target) are the two DOCUMENTS write
# templates; both end in the IDENTICAL final MERGE line, so the substring is
# unambiguous across either template (matches
# scripts/verify-ifa-fault-injection.sh:302
# documentation_edge_operation_match, and
# scripts/lib/test-ifa-fault-injection-documentation-cases.sh:25's own pin).
# shared_cell: scripts/lib/ifa_fault_injection_driver.sh:99-100 drives it
# unconditionally in drive_all_cassettes, and the registry-driven
# determinism loop runs its drive/assert every N. cell_kind:
# blocker_kind=ack_barrier is NOT one of the generic dispatcher's three
# shapes (scripts/lib/ifa_fault_generic_cells.sh:473 dies on it) => custom,
# independently confirmed live: ifa_fault_injection_documentation_cells.sh's
# kill cell is the family's own hand-written function, dispatched to (not
# built) by cell_killworker_family per that same file's header.
IFA_FAMILY_PIN_ANCHOR="MERGE (section)-[rel:DOCUMENTS]->(target)"
IFA_FAMILY_PIN_SHARED_CELL=1
IFA_FAMILY_PIN_CELL_KIND="custom"
