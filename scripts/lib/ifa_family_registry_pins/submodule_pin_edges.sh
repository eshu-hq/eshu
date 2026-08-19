#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# submodule_pin_edges hand-derived pin (#6002). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh, which owns the
# hand-authored-literal rule, the totality diff, and the comparison against
# scripts/lib/ifa_family_registry.sh -- read that file's header before
# touching this one. Every value below is HAND-TYPED literal text, derived by
# reading the citations inline; it is never sourced, generated, or read back
# out of the registry.

# go/internal/reducer/submodule_pin_materialization.go:28-37 declares
# SubmodulePinEdgeMaterializationHandler with FactLoader, EdgeWriter
# (SharedProjectionEdgeWriter), PriorGenerationCheck, and Instruments fields
# -- and NO IntentWriter, anywhere in the struct or the file (`rg -c
# IntentWriter go/internal/reducer/submodule_pin_materialization.go` returns
# nothing, exit 1). That absence is the load-bearing fact. Handle() calls
# only h.EdgeWriter.RetractEdges (:82) and h.EdgeWriter.WriteEdges (:92); it
# never touches shared_projection_intents, so shared_intent_lock is the one
# kind this family provably cannot use. wait_stage=handler, wait_key
# ="submodule_pin" (go/internal/reducer/intent.go:76-82
# DomainSubmodulePin Domain = "submodule_pin" -- the routed queue domain
# Handle() checks at :41, NOT DomainSubmodulePinEdges, the separate
# ProjectionDomain label WriteEdges/RetractEdges take at :83/:94).
#
# Which of the two REMAINING kinds is correct (ack_barrier vs a
# table_lock:<name>) cannot be settled from the handler shape alone -- an
# EdgeWriter-only handler admits either, per
# checkFamilyBlockerLockstep (go/internal/reducer/materialized_edge_family_blocker_shape_test.go:283).
# No live fault cell exists for this family yet (no
# ifa_fault_injection_submodule_pin_cells.sh), so unlike
# codeowners_ownership_edges' pin -- which resolves this from ITS landed
# cell's real lock call (ifa_codeowners_start_fact_records_lock) -- there is
# no cell call site here to read yet. This pin instead derives the SAME
# conclusion from the handler's own fact-load entry point, which is provably
# identical to codeowners' in shape: loadSubmodulePinMaterializationFacts
# (go/internal/reducer/submodule_pin_delta_scope.go:38-55) calls
# loadFactsForKinds -> loader.ListFactsByKind
# (go/internal/storage/postgres/facts_filtered.go:140
# FactStore.ListFactsByKind) -- the identical FactLoader call path
# codeowners_ownership_edges' own handler uses for its first synchronous
# read. Both families are EdgeWriter-only handlers whose first read is a
# fact-kind-scoped ListFactsByKind call; a table lock on fact_records is what
# a kill cell would need to engage to hold either handler in flight on that
# first read, for the identical reason. table_lock:fact_records, not
# ack_barrier: this handler's blocking dependency is a fact READ, not an ACK
# transition a trigger could gate the way documentation_edges' ack_barrier
# mechanism does.
#
# THIS IS A WEAKER DERIVATION THAN CODEOWNERS' PIN, AND SAID SO PLAINLY: it
# reasons from handler-shape analogy, not from a live cell's own lock SQL,
# because no cell exists yet to read. Re-derive this pin from the real fault
# cell's call site once ifa_fault_injection_submodule_pin_cells.sh lands, the
# same way codeowners_ownership_edges' pin was corrected in place once ITS
# landed cell was re-read (that pin's own history section documents the
# earlier, wrong "ack_barrier by analogy" conclusion this family's pin is
# now careful not to repeat pre-cell).
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="submodule_pin"

# go/internal/storage/cypher/canonical_submodule_edges.go:29-35
# (batchCanonicalSubmodulePinEdgeCypher) is the family's only PINS_SUBMODULE
# write template; that file's own header (:19-28) explains why the MERGE key
# is NOT the bare relationship type -- it deliberately includes
# {path: row.submodule_path} so a parent repository pinning the same target
# at two different paths, or two different targets, does not collapse onto
# one edge -- so the anchor substring must include that property map, not
# stop at the bare `]->(target)`.
IFA_FAMILY_PIN_ANCHOR="MERGE (parent)-[rel:PINS_SUBMODULE {path: row.submodule_path}]->(target)"
# shared_cell: 1. This family's cassette
# (testdata/cassettes/submodulepin/ifa-submodule-pin-family.json) and
# expected-edge set
# (go/internal/ifa/testdata/submodulepin/ifa-submodule-pin-family-expected-edges.json)
# land in the SAME change as this pin (#6002), and
# scripts/lib/ifa_submodule_pin_live.sh's drive/assert helpers are wired into
# verify-ifa-determinism.sh's shared N={1,2,4} loop in that same change --
# unlike codeowners_ownership_edges, there is no interim "row exists but not
# yet driven" state for this family to have recorded.
#
# The FAULT side is NOT shared and does not exist yet: no
# ifa_fault_injection_submodule_pin_cells.sh has landed (confirmed by its
# continued listing in materializedEdgeFamilyNotYetInRegistry,
# go/internal/reducer/materialized_edge_family_blocker_shape_test.go:173).
# shared_cell describes the determinism shared cell only, per the schema.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: custom, on the team's stated dispatch intent for when the fault
# cells land (hand-written functions dispatched by name, mirroring
# codeowners_ownership_edges and documentation_edges, not the generic
# table_lock dispatcher) -- NOT yet verifiable from a real call site, because
# no fault cell exists. This is the one field on this pin that is a stated
# intent rather than an observed fact; re-derive it from
# ifa_fault_injection_submodule_pin_cells.sh's real dispatch once that file
# lands, exactly as the schema's own rule demands ("Verify from the gate's
# call sites, never by inferring").
IFA_FAMILY_PIN_CELL_KIND="custom"
