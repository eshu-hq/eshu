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

# go/internal/reducer/rationale/materialization.go declares
# IntentWriter (formerly RationaleEdgeIntentWriter); MaterializationHandler
# embeds it as IntentWriter; Handle() calls h.IntentWriter.UpsertIntents(...).
# Same shape as
# code_calls => blocker_kind=shared_intent_lock. Confirmed live, one hop
# through the generic dispatcher rather than a direct call:
# cell_killworker_rationale (scripts/lib/ifa_fault_injection_rationale_cells.sh)
# is now a one-line delegation to cell_killworker_family, whose
# _ifa_generic_wait_for_claimed helper (scripts/lib/ifa_fault_generic_cells.sh)
# calls ifa_fault_wait_for_claimed against fact_work_items domain
# "rationale_materialization" -- the wait_key this family's own row declares
# (scripts/lib/ifa_family_registry/rows/04_rationale_edges.sh) --
# (go/internal/reducer/intent.go:69 DomainRationaleMaterialization Domain =
# "rationale_materialization") -- handler stage.
IFA_FAMILY_PIN_BLOCKER_KIND="shared_intent_lock"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="rationale_materialization"

# go/internal/storage/cypher/canonical_rationale_edges.go:33-45
# (batchCanonicalRationaleExplainsEdgeCypher) is the family's only EXPLAINS
# write template (matches the registry row's own literal --
# scripts/lib/ifa_family_registry/rows/04_rationale_edges.sh's
# IFA_FAMILY_ANCHOR[rationale_edges] -- read by the generic dispatcher's
# _ifa_generic_cell_failgraphwrite via ifa_family_anchor; there is no
# dedicated *_operation_match shell var for this family any more now that it
# has no hand-written fault cells of its own). shared_cell:
# scripts/lib/ifa_fault_injection_driver.sh:101-102 drives it
# unconditionally in drive_all_cassettes, and
# scripts/verify-ifa-determinism.sh's registry drive and assert loops
# (`while IFS= read -r family; do ... done < <(ifa_family_registry_names)`,
# calling ifa_family_registry_drive and ifa_family_registry_assert) run its
# drive/assert every N.
# cell_kind: blocker_kind=shared_intent_lock => generic.
IFA_FAMILY_PIN_ANCHOR="MERGE (rationale)-[rel:EXPLAINS]->(target)"
IFA_FAMILY_PIN_SHARED_CELL=1
IFA_FAMILY_PIN_CELL_KIND="generic"
