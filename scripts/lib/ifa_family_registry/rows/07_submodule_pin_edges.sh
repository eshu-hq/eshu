#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# submodule_pin_edges row (#6002). See ../../ifa_family_registry.sh for the
# schema and every array declaration this file assigns into.

# go/internal/reducer/submodule_pin_materialization.go:28-37 declares
# SubmodulePinEdgeMaterializationHandler with FactLoader, EdgeWriter
# (SharedProjectionEdgeWriter), PriorGenerationCheck, and Instruments -- and
# NO IntentWriter, anywhere in the struct or the file (`rg -c IntentWriter
# go/internal/reducer/submodule_pin_materialization.go` returns nothing,
# exit 1). Handle() calls only h.EdgeWriter.RetractEdges (:82) and
# h.EdgeWriter.WriteEdges (:92); it never touches shared_projection_intents,
# so shared_intent_lock is the one kind this family provably cannot use --
# the same shape codeowners_ownership_edges' row documents, and
# checkFamilyBlockerLockstep
# (go/internal/reducer/materialized_edge_family_blocker_shape_test.go:284)
# rejects shared_intent_lock for exactly this reason, naming ack_barrier or a
# table_lock:<name> the handler really touches as the alternatives.
#
# The handler's first read is the fact load: loadSubmodulePinMaterializationFacts
# (go/internal/reducer/submodule_pin_delta_scope.go:38-55) calls
# loadFactsForKinds -> loader.ListFactsByKind, the identical FactLoader path
# codeowners_ownership_edges' row already established reads fact_records
# first (go/internal/storage/postgres/facts_filtered.go:140
# FactStore.ListFactsByKind). Verified against the real cell, not merely
# analogy: scripts/lib/ifa_fault_injection_submodule_pin_cells.sh's
# ifa_submodule_pin_start_fact_records_lock takes `LOCK TABLE fact_records IN
# ACCESS EXCLUSIVE MODE`, the identical mechanism codeowners_ownership_edges'
# landed cell uses.
IFA_FAMILY_BLOCKER_KIND[submodule_pin_edges]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[submodule_pin_edges]="handler"
# go/internal/reducer/intent.go:76-82 declares
# DomainSubmodulePin Domain = "submodule_pin", and
# submodule_pin_materialization.go:41-46 routes Handle() on exactly that
# domain (`if intent.Domain != DomainSubmodulePin`). DomainSubmodulePinEdges
# ("submodule_pin_edges") is a DIFFERENT constant -- the ProjectionDomain
# label passed to h.EdgeWriter.WriteEdges/RetractEdges (:83,:94), not the
# routed queue domain. wait_key must be the routed Domain, mirroring
# codeowners_ownership_edges' row (which pins "codeowners_ownership", not
# "codeowners_ownership_edges", for the identical reason).
IFA_FAMILY_WAIT_KEY[submodule_pin_edges]="submodule_pin"
# Wired into the shared N={1,2,4} determinism cell from the start (#6002):
# unlike codeowners_ownership_edges (whose shared_cell flipped 0->1 in a
# later PR, #6160), this family's cassette
# (testdata/cassettes/submodulepin/ifa-submodule-pin-family.json) and
# expected-edge set
# (go/internal/ifa/testdata/submodulepin/ifa-submodule-pin-family-expected-edges.json)
# land in the same change as this row, so there is no "NOT YET WIRED"
# interim state to record here.
#
# The fault side is separate and is NOT drive_all_cassettes:
# scripts/lib/ifa_fault_injection_submodule_pin_cells.sh drives its own
# cassette from its own cells (mirroring codeowners_ownership_edges), the
# same "a new family's own cells drive its cassette" convention every
# FAULT_SHARED_DRIVE=0 family follows. shared_cell describes the determinism
# shared cell only, per the schema comment above.
IFA_FAMILY_SHARED_CELL[submodule_pin_edges]=1
# Dispatch metadata for the determinism drive/assert loop. Signatures follow
# the majority labeled shape -- ifa_submodule_pin_drive(label, bin_dir,
# cassette, workers, log_dir) and ifa_submodule_pin_assert(label, bin_dir,
# expected) -- verified against scripts/lib/ifa_submodule_pin_live.sh, which
# this same change adds mirroring scripts/lib/ifa_codeowners_live.sh exactly.
IFA_FAMILY_DRIVE_FN[submodule_pin_edges]="ifa_submodule_pin_drive"
IFA_FAMILY_ASSERT_FN[submodule_pin_edges]="ifa_submodule_pin_assert"
IFA_FAMILY_CASSETTE_VAR[submodule_pin_edges]="submodule_pin_cassette"
IFA_FAMILY_EXPECTED_VAR[submodule_pin_edges]="submodule_pin_expected_edges"
# go/internal/storage/cypher/canonical_submodule_edges.go's
# batchCanonicalSubmodulePinEdgeCypher (:29-35) is the family's only
# PINS_SUBMODULE write template. That file's own header (:19-28) explains why
# the MERGE key is NOT the bare relationship type -- it deliberately includes
# {path: row.submodule_path} so a parent repository pinning the same target
# at two different paths (or two different targets) does not collapse onto
# one edge -- mirroring codeowners_ownership_edges' identical reasoning for
# {pattern, source_path}. cell_kind=custom, so the live cell reads a
# family-specific match string
# (scripts/verify-ifa-fault-injection.sh's submodule_pin_edge_operation_match,
# a bare prefix) rather than this field -- this row still records the
# precise anchor substring including the property map, not the bare
# `]->(target)`, mirroring codeowners' identical field/live-string split.
IFA_FAMILY_ANCHOR[submodule_pin_edges]="MERGE (parent)-[rel:PINS_SUBMODULE {path: row.submodule_path}]->(target)"
# cell_kind=custom, verified against the real dispatch call site, not
# inferred: scripts/lib/ifa_fault_injection_submodule_pin_cells.sh's three
# cells are hand-written functions dispatched by name
# (ifa_fault_shard_run cell_killworker_submodule_pin, etc.), never through
# cell_killworker_family/cell_failgraphwrite_family.
#
# custom FOR A DIFFERENT REASON than codeowners_ownership_edges' row states
# for itself -- codeowners is custom because #6160 hand-wired its cells
# before the generic dispatcher existed and never migrated them (a
# dispatch-history reason). This family is custom because the generic
# table_lock path is PROVEN BROKEN for fact_records:
# scripts/lib/ifa_fault_generic_table_lock.sh's
# _ifa_generic_require_table_domain_written runs `SELECT count(*) FROM
# <table> WHERE domain = '<wait_key>'`, assuming the locked table has a
# domain column. fact_records does not:
# go/internal/storage/postgres/migrations/003_fact_records.sql's CREATE
# TABLE plus its own four ALTER TABLE ADD COLUMN statements list fact_id,
# scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
# collector_kind, fencing_token, source_confidence, source_system,
# source_fact_key, source_uri, source_record_id, observed_at, ingested_at,
# is_tombstone, payload -- no domain column anywhere, and no later
# migration adds one (`rg "ALTER TABLE fact_records"
# go/internal/storage/postgres/migrations/*.sql` returns only 003's own
# four). ifa_fault_generic_table_lock.sh's own header already names
# codeowners_ownership_edges (also table_lock:fact_records) as the worked
# example of this exact mismatch; this family shares the same table and
# therefore the same mismatch, so cell_kind=custom records a proven
# incompatibility, not a dispatch-history accident or a stated intent.
IFA_FAMILY_CELL_KIND[submodule_pin_edges]="custom"

# No IFA_FAMILY_RETRY_BASELINE_VAR row: that field is required only for
# shared_intent_lock families, whose generic kill cell compares against it.
# This family is table_lock:fact_records and cell_kind=custom, so its own
# cells own their baseline the way codeowners_ownership_edges' cells do, and
# the generic precondition never runs for it.

# Recorded even though nothing reads it for this family: handler_go_file is
# consumed only by _ifa_generic_require_intent_writer, which runs for
# shared_intent_lock + generic families, and this one is table_lock:fact_records
# + custom. Kept present for the same reason codeowners_ownership_edges' row
# keeps it: it is the file every other field here is derived from, and a
# family that later moves to generic dispatch needs it present rather than
# remembered.
IFA_FAMILY_HANDLER_GO_FILE[submodule_pin_edges]="go/internal/reducer/submodule_pin_materialization.go"

# NOT in drive_all_cassettes: per repo convention (see the shared_cell field
# comment in ../ifa_family_registry.sh's schema header), a new family's own
# cells drive its cassette; the shared fault drive is never extended for a
# new family.
IFA_FAMILY_FAULT_SHARED_DRIVE[submodule_pin_edges]="0"

IFA_FAMILY_NAMES+=(submodule_pin_edges)
