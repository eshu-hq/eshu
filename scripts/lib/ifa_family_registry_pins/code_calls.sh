#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# code_calls hand-derived pin (#6147 PR-0 family registry). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh, which owns the
# hand-authored-literal rule, the totality diff, and the comparison against
# scripts/lib/ifa_family_registry.sh -- read that file's header before
# touching this one. Every value below is HAND-TYPED literal text, derived by
# reading the citations inline; it is never sourced, generated, or read back
# out of the registry.

# go/internal/reducer/code_call_materialization.go:32 declares
# CodeCallIntentWriter; :47 embeds it as IntentWriter; :224 calls
# h.IntentWriter.UpsertIntents(...) inside Handle(). Same shape as
# sql_relationships => blocker_kind=shared_intent_lock. Confirmed live, one
# hop through the generic dispatcher rather than a direct call:
# cell_killworker_code_calls (scripts/lib/ifa_fault_injection_code_call_cells.sh)
# is now a one-line delegation to cell_killworker_family, whose
# _ifa_generic_wait_for_claimed helper (scripts/lib/ifa_fault_generic_cells.sh)
# calls ifa_fault_wait_for_claimed against fact_work_items domain
# "code_call_materialization" -- the wait_key this family's own row declares
# (scripts/lib/ifa_family_registry/rows/02_code_calls.sh) --
# (go/internal/reducer/intent.go:42 DomainCodeCallMaterialization Domain =
# "code_call_materialization") -- handler stage.
IFA_FAMILY_PIN_BLOCKER_KIND="shared_intent_lock"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="code_call_materialization"

# go/internal/storage/cypher/canonical_code_call_edges.go:67-75
# (batchCanonicalCodeCallUpsertCypher) is the live CALLS write template;
# canonical.go:99-109's single-row canonicalCodeCallUpsertCypher is
# explicitly commented "legacy... has no production caller" at that same
# file, so it is not the source of truth despite sharing the same MERGE
# text. anchor (matches the registry row's own literal --
# scripts/lib/ifa_family_registry/rows/02_code_calls.sh's
# IFA_FAMILY_ANCHOR[code_calls] -- read by the generic dispatcher's
# _ifa_generic_cell_failgraphwrite via ifa_family_anchor; there is no
# dedicated *_operation_match shell var for this family any more now that it
# has no hand-written fault cells of its own). shared_cell:
# scripts/lib/ifa_fault_injection_driver.sh:97-98's drive_all_cassettes
# calls ifa_code_call_drive unconditionally for every cell, and
# scripts/verify-ifa-determinism.sh's `for family in
# $(ifa_family_registry_names); do ... ifa_family_registry_drive` loop
# (header comment at :338, loop body at :347-353) is what this family's
# determinism-gate drive now runs through. cell_kind:
# blocker_kind=shared_intent_lock is generic-dispatcher-supported =>
# generic.
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:CALLS]->(target)"
IFA_FAMILY_PIN_SHARED_CELL=1
IFA_FAMILY_PIN_CELL_KIND="generic"
