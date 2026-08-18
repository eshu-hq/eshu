#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# rationale_edges hand-derived pin (#6147 PR-0 family registry). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh, which owns the
# hand-authored-literal rule, the totality diff, and the comparison against
# scripts/lib/ifa_family_registry.sh -- read that file's header before
# touching this one. Every value below is HAND-TYPED literal text, derived by
# reading the citations inline; it is never sourced, generated, or read back
# out of the registry.

# go/internal/reducer/rationale_edge_materialization.go:26 declares
# RationaleEdgeIntentWriter; :39 embeds it as IntentWriter; :98 calls
# h.IntentWriter.UpsertIntents(...) inside Handle(). Same shape as
# code_calls => blocker_kind=shared_intent_lock. Confirmed live:
# scripts/lib/ifa_fault_injection_rationale_cells.sh:22-24 calls
# ifa_fault_wait_for_claimed against fact_work_items domain
# "rationale_materialization" (go/internal/reducer/intent.go:69
# DomainRationaleMaterialization Domain = "rationale_materialization") --
# handler stage.
IFA_FAMILY_PIN_BLOCKER_KIND="shared_intent_lock"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="rationale_materialization"

# go/internal/storage/cypher/canonical_rationale_edges.go:33-45
# (batchCanonicalRationaleExplainsEdgeCypher) is the family's only EXPLAINS
# write template (matches scripts/verify-ifa-fault-injection.sh:294
# rationale_edge_operation_match). shared_cell:
# scripts/lib/ifa_fault_injection_driver.sh:101-102 drives it
# unconditionally in drive_all_cassettes, and
# scripts/verify-ifa-determinism.sh:338-342/386-390's registry-driven loop
# runs its drive/assert every N. cell_kind: blocker_kind=shared_intent_lock
# => generic.
IFA_FAMILY_PIN_ANCHOR="MERGE (rationale)-[rel:EXPLAINS]->(target)"
IFA_FAMILY_PIN_SHARED_CELL=1
IFA_FAMILY_PIN_CELL_KIND="generic"
