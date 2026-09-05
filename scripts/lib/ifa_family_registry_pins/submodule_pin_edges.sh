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
# checkFamilyBlockerLockstep (go/internal/reducer/materialized_edge_family_blocker_shape_test.go:284).
# RE-DERIVED FROM THE LANDED CELL, not analogy: this pin was originally
# written before scripts/lib/ifa_fault_injection_submodule_pin_cells.sh
# existed, reasoning only from handler-shape similarity to
# codeowners_ownership_edges -- flagged in-file at the time as weaker than
# codeowners' own pin for exactly that reason. That cell now exists.
# ifa_submodule_pin_start_fact_records_lock (that file) takes `LOCK TABLE
# fact_records IN ACCESS EXCLUSIVE MODE`, so the handler blocks on its FIRST
# synchronous read (loadSubmodulePinMaterializationFacts ->
# loadFactsForKinds -> loader.ListFactsByKind ->
# go/internal/storage/postgres/facts_filtered.go:140
# FactStore.ListFactsByKind), never on shared_projection_intents the handler
# never writes. table_lock:fact_records, not ack_barrier: the blocking
# dependency is a fact READ, not an ACK transition a trigger could gate the
# way documentation_edges' ack_barrier mechanism does.
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
# The FAULT side is NOT shared: scripts/lib/ifa_fault_injection_submodule_pin_cells.sh
# drives its own cassette from its own cells, the same "a new family's own
# cells drive its cassette" convention codeowners_ownership_edges follows.
# shared_cell describes the determinism shared cell only, per the schema.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: custom, verified from the real dispatch call site, not
# inferred: scripts/lib/ifa_fault_injection_submodule_pin_cells.sh's three
# cells (cell_baseline_submodule_pin, cell_killworker_submodule_pin,
# cell_failgraphwrite_submodule_pin) are hand-written functions dispatched by
# name from scripts/verify-ifa-fault-injection.sh
# (`ifa_fault_shard_run cell_killworker_submodule_pin`, etc.), never through
# cell_killworker_family/cell_failgraphwrite_family.
#
# custom FOR A DIFFERENT REASON THAN CODEOWNERS' PIN STATES FOR ITSELF, and
# this pin is deliberately not copying that reasoning: codeowners_ownership_edges
# is custom because #6160 hand-wired its cells before
# scripts/lib/ifa_fault_generic_cells.sh existed and never migrated them (a
# dispatch-history reason, true of codeowners and false of this family, which
# was built AFTER the generic dispatcher landed). This family is custom
# because the generic table_lock mechanism is PROVEN BROKEN for fact_records:
# scripts/lib/ifa_fault_generic_table_lock.sh's
# _ifa_generic_require_table_domain_written runs `SELECT count(*) FROM
# <table> WHERE domain = '<wait_key>'`, assuming a domain column that
# fact_records does not have. Verified directly against the schema, not
# against that file's own claim about it:
# go/internal/storage/postgres/migrations/003_fact_records.sql's CREATE
# TABLE plus its own four ALTER TABLE ADD COLUMN statements enumerate
# fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
# schema_version, collector_kind, fencing_token, source_confidence,
# source_system, source_fact_key, source_uri, source_record_id, observed_at,
# ingested_at, is_tombstone, payload -- no domain column, and no later
# migration adds one (`rg "ALTER TABLE fact_records"
# go/internal/storage/postgres/migrations/*.sql` returns only 003's own
# four). ifa_fault_generic_table_lock.sh's own header names
# codeowners_ownership_edges (also table_lock:fact_records) as the worked
# example of this exact mismatch; this family shares the same table and
# inherits the same mismatch on its own evidence, independently confirmed
# against the migration rather than taken on that header's word.
IFA_FAMILY_PIN_CELL_KIND="custom"
