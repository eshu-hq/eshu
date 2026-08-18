#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# codeowners_ownership_edges hand-derived pin (#6147 PR-0 family registry).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh,
# which owns the hand-authored-literal rule, the totality diff, and the
# comparison against scripts/lib/ifa_family_registry.sh -- read that file's
# header before touching this one. Every value below is HAND-TYPED literal
# text, derived by reading the citations inline; it is never sourced,
# generated, or read back out of the registry.

# go/internal/reducer/codeowners_ownership_materialization.go:26-35 declares
# CodeownersOwnershipEdgeMaterializationHandler with FactLoader, EdgeWriter,
# and PriorGenerationCheck fields ONLY -- no IntentWriter field anywhere in
# the struct or the file (`rg -c IntentWriter
# go/internal/reducer/codeowners_ownership_materialization.go` returns
# nothing). Handle() calls only h.EdgeWriter.WriteEdges/RetractEdges (lines
# 86, 138, 145); it never touches shared_projection_intents. Architecturally
# identical to documentation_edges => the CORRECT declaration is
# blocker_kind=ack_barrier, wait_stage=handler,
# wait_key="codeowners_ownership" (go/internal/reducer/intent.go:75
# DomainCodeownersOwnership Domain = "codeowners_ownership").
#
# *** KNOWN DISCREPANCY, REPORTED HERE, NOT SILENTLY CONFORMED TO ***
# The LANDED cell (scripts/lib/ifa_fault_injection_codeowners_cells.sh,
# ifa_codeowners_start_intent_lock, proven by
# scripts/lib/test-ifa-fault-injection-codeowners-cases.sh:23-30 to hold the
# "shared_projection_intents blocker") uses shared_intent_lock, copying the
# code_calls/sql_relationships/rationale_edges pattern even though this
# handler has no IntentWriter and that lock is architecturally vacuous for
# it -- the same defect class as documentation_edges' now-removed
# ifa_documentation_start_intent_lock (#6149 follow-up item 8). This pin
# asserts the CORRECT value (ack_barrier); it does not conform to the landed
# cell's mistake. If ifa_family_registry.sh instead declares
# shared_intent_lock for this family (matching the landed, wrong cell), that
# is the registry describing a bug, not this pin being stale, and the fix
# belongs in the cell + registry together, not here.
IFA_FAMILY_PIN_BLOCKER_KIND="ack_barrier"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="codeowners_ownership"

# go/internal/storage/cypher/canonical_codeowners_edges.go:32-38
# (batchCanonicalCodeownersOwnershipEdgeCypher) is the family's only
# DECLARES_CODEOWNER write template; that file's own header (lines 16-31)
# explains why the MERGE key is NOT the bare relationship type -- it
# deliberately includes {pattern, source_path} so two distinct CODEOWNERS
# rules naming the same team do not collapse onto one edge -- so the anchor
# substring must include that property map, not stop at the bare
# `]->(team)`. shared_cell: this family has no cassette/expected-edge entry
# anywhere in ifa_family_fixtures.sh (`rg -c codeowners
# scripts/lib/ifa_family_fixtures.sh` returns nothing) and
# scripts/lib/ifa_fault_injection_driver.sh's drive_all_cassettes never
# mentions it either -- there is no cassette for the shared loop to drive,
# so shared_cell=0 is architecturally forced, not a preference. cell_kind:
# blocker_kind=ack_barrier => custom, same rule as documentation_edges;
# independently confirmed live: this family's kill cell
# (ifa_fault_injection_codeowners_cells.sh) is a hand-written function, the
# same shape documentation_edges' custom cell takes, though (per the KNOWN
# DISCREPANCY note above) built on the wrong lock target today.
IFA_FAMILY_PIN_ANCHOR="MERGE (repo)-[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]->(team)"
IFA_FAMILY_PIN_SHARED_CELL=0
IFA_FAMILY_PIN_CELL_KIND="custom"
