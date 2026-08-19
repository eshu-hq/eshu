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
# (go/internal/reducer/materialized_edge_family_blocker_shape_test.go:283)
# rejects shared_intent_lock for exactly this reason, naming ack_barrier or a
# table_lock:<name> the handler really touches as the alternatives.
#
# The handler's first read is the fact load: loadSubmodulePinMaterializationFacts
# (go/internal/reducer/submodule_pin_delta_scope.go:38-55) calls
# loadFactsForKinds -> loader.ListFactsByKind, the identical FactLoader path
# codeowners_ownership_edges' row already established reads fact_records
# first (go/internal/storage/postgres/facts_filtered.go:140
# FactStore.ListFactsByKind). Both EdgeWriter-only handlers share the same
# fact-load entry point, so this row takes the same table_lock this family's
# closest structural sibling took, for the same reason: it is what a kill
# cell can actually engage to hold the handler in flight on its first
# synchronous read.
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
# The fault side is separate and is NOT drive_all_cassettes, and is not yet
# built at all: no ifa_fault_injection_submodule_pin_cells.sh exists as of
# this writing (see materializedEdgeFamilyNotYetInRegistry in
# go/internal/reducer/materialized_edge_family_blocker_shape_test.go, which
# still lists DomainSubmodulePinEdges pending exactly that file). shared_cell
# describes the determinism shared cell only, per the schema comment above.
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
# {pattern, source_path}. This family is cell_kind=custom, so the live cell
# (once it exists) will read a family-specific match string the way
# codeowners' row documents its own generic-dispatch exemption; this row
# still records the precise anchor substring including the property map, not
# the bare `]->(target)`.
IFA_FAMILY_ANCHOR[submodule_pin_edges]="MERGE (parent)-[rel:PINS_SUBMODULE {path: row.submodule_path}]->(target)"
# No fault cells exist for this family yet (see the shared_cell comment
# above), so cell_kind cannot yet be verified from a real dispatch call site
# the way the schema's own rule demands ("Verify from the gate's call sites,
# never by inferring"). Recorded as custom on the team's stated intent for
# when the cells land -- this family's cells will be hand-written functions
# dispatched by name, the same shape codeowners_ownership_edges and
# documentation_edges take, not the generic table_lock dispatcher
# (ifa_fault_generic_table_lock.sh) -- NOT because table_lock is an
# unsupported generic shape (it is, per deployable_unit_edges' row), but
# because that is the planned dispatch shape. Re-verify this field against
# the real fault cell's call site once ifa_fault_injection_submodule_pin_cells.sh
# lands, the same way codeowners_ownership_edges' row was corrected in place
# once ITS landed cell was re-read.
IFA_FAMILY_CELL_KIND[submodule_pin_edges]="custom"

# No IFA_FAMILY_RETRY_BASELINE_VAR row: that field is required only for
# shared_intent_lock families, whose generic kill cell compares against it.
# This family is table_lock:fact_records and cell_kind=custom, so its own
# cells (once written) own their baseline the way codeowners_ownership_edges'
# cells do, and the generic precondition never runs for it.

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
# new family, and this family has no fault cells at all yet.
IFA_FAMILY_FAULT_SHARED_DRIVE[submodule_pin_edges]="0"

IFA_FAMILY_NAMES+=(submodule_pin_edges)
