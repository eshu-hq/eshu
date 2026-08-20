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
# PriorGenerationCheck and Instruments fields -- and NO IntentWriter, anywhere
# in the struct or the file (`rg -c IntentWriter
# go/internal/reducer/codeowners_ownership_materialization.go` returns
# nothing). That absence is the load-bearing fact. Handle() calls only
# h.EdgeWriter.WriteEdges/RetractEdges (lines 86, 138, 145); it never touches
# shared_projection_intents, so shared_intent_lock is the one kind this family
# provably cannot use. wait_stage=handler, wait_key="codeowners_ownership"
# (go/internal/reducer/intent.go:75
# DomainCodeownersOwnership Domain = "codeowners_ownership").
#
# Which of the two REMAINING kinds is correct is settled further down, from the
# landed cell rather than from the handler shape: an EdgeWriter-only handler
# admits either ack_barrier or a table_lock:<name>, and the handler alone
# cannot choose between them. An earlier version of this paragraph concluded
# "ack_barrier" here, by analogy to documentation_edges, and then the
# derivation below pinned table_lock:fact_records -- two readings in one pin
# file, which is the property a pin file exists to not have.
#
# *** RE-DERIVED, NOT JUST RE-CITED: the vacuous-lock discrepancy this
# section used to report is FIXED on the landed cell; a different question
# now stands in its place -- see the last paragraph below. ***
# The LANDED cell (scripts/lib/ifa_fault_injection_codeowners_cells.sh) no
# longer calls ifa_codeowners_start_intent_lock at all -- that function does
# not exist in the file any more. It now calls
# ifa_codeowners_start_fact_records_lock, which takes `LOCK TABLE
# fact_records IN ACCESS EXCLUSIVE MODE` so the handler blocks on its FIRST
# synchronous read (the fact load), not on a shared_projection_intents write
# the handler never performs. That file's own header states this was PROVEN
# live (#5992, 2026-08-17): the claimed row sat blocked for 14 consecutive
# 1s samples with reducer backends visibly waiting on pg_locks for
# fact_records, then reached succeeded within 1s of the lock's release. This
# resolves the vacuous-lock defect the earlier version of this section
# reported (shared_intent_lock copied from code_calls/sql_relationships/
# rationale_edges onto a handler with no IntentWriter) -- confirmed by
# scripts/lib/test-ifa-fault-injection-codeowners-cases.sh, which now stubs
# and drives ifa_codeowners_start_fact_records_lock /
# ifa_codeowners_release_fact_records_lock, not the removed intent-lock pair.
#
# RESOLVED (was an open question on this pin): the schema
# (ifa_family_registry.sh's field-comment block) lists table_lock:<tablename>
# and ack_barrier as distinct blocker_kind categories, and the landed mechanism
# is literally a table lock on fact_records blocking a read -- not a trigger
# blocking the claimed->succeeded ACK transition the way documentation_edges'
# mechanism does. Resolved to table_lock:fact_records on the schema's own rule
# for the sibling cell_kind field: it records DISPATCH REALITY, "verify from
# the gate's call sites, never by inferring". The call site is
# ifa_fault_injection_codeowners_cells.sh:190 invoking
# ifa_codeowners_start_fact_records_lock (:76), whose SQL is
# `LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE` (:79).
# Derived here from those call sites, NOT read back out of the registry row.
# checkFamilyBlockerLockstep
# (go/internal/reducer/materialized_edge_family_blocker_shape_test.go)
# accepts either ack_barrier or a table_lock:<name> for an EdgeWriter-only
# handler, so this is not a value the Go side could have disambiguated.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="codeowners_ownership"

# go/internal/storage/cypher/canonical_codeowners_edges.go:32-38
# (batchCanonicalCodeownersOwnershipEdgeCypher) is the family's only
# DECLARES_CODEOWNER write template; that file's own header (lines 16-31)
# explains why the MERGE key is NOT the bare relationship type -- it
# deliberately includes {pattern, source_path} so two distinct CODEOWNERS
# rules naming the same team do not collapse onto one edge -- so the anchor
# substring must include that property map, not stop at the bare
# `]->(team)`. cell_kind: custom, derived from the gate's call sites -- this
# family's fault cells are hand-written functions dispatched by name
# (ifa_fault_injection_codeowners_cells.sh), the same shape documentation_edges'
# custom cell takes, not the generic dispatcher.
#
# A superseded shared_cell derivation used to sit in this paragraph, claiming
# `rg -c codeowners scripts/lib/ifa_family_fixtures.sh` returns nothing and that
# shared_cell=0 was therefore "architecturally forced". That command returns 6:
# codeowners_cassette and codeowners_expected_edges are at
# ifa_family_fixtures.sh:73-74, guarded at :96-97. #6160 added them, and the
# derivation below pins 1. Removed rather than left standing beside the correct
# one -- a pin file with two readings has lost the property it exists for.
IFA_FAMILY_PIN_ANCHOR="MERGE (repo)-[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]->(team)"
# shared_cell: 1, re-derived after #6160 merged. That PR added this family's
# cassette and expected-edge entries to scripts/lib/ifa_family_fixtures.sh and
# its drive/assert helpers to scripts/lib/ifa_codeowners_live.sh, and drove it
# in the determinism gate's shared N={1,2,4} cell. This pin read 0 before that,
# which was correct then: the family had no shared-cell drive at all.
# Derived from the gate, not from the registry row: verify-ifa-determinism.sh
# drives every registered shared_cell family through ifa_family_registry_drive
# in the shared N-loop, and this family is registered there now.
# Note the FAULT side is not shared: #6160 gave it a scoped baseline plus two
# recovery cells (the deployable_unit shape), because its edges need a
# maintenance pass the shared cells do not run. shared_cell describes the
# determinism shared cell only.
IFA_FAMILY_PIN_SHARED_CELL=1
IFA_FAMILY_PIN_CELL_KIND="custom"
