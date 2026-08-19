#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# codeowners_ownership_edges row. See ../../ifa_family_registry.sh for the
# schema and every array declaration this file assigns into.

# Records DISPATCH REALITY, per the schema's own rule for cell_kind
# (ifa_family_registry.sh: "Verify from the gate's call sites, never by
# inferring"): the landed cell holds an ACCESS EXCLUSIVE lock on fact_records,
# the table this EdgeWriter-only handler reads first.
# ifa_fault_injection_codeowners_cells.sh:76 defines
# ifa_codeowners_start_fact_records_lock, whose SQL is
# `LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE` (:79); it is invoked from
# cell_killworker_codeowners at :190 and released at :207.
#
# An earlier version of this row described a shared_projection_intents lock via
# a function named ifa_codeowners_start_intent_lock, and told the reader not to
# "fix" the cell to match. Both were true of a shape that #5992 removed and
# #6160 replaced: that function name exists nowhere in scripts/ today, and
# `rg 'shared_projection_intents' scripts/lib/ifa_fault_injection_codeowners_cells.sh`
# returns nothing. The instruction was the dangerous part -- following it meant
# reintroducing the vacuous lock the fix removed.
#
# shared_intent_lock is the one kind this family genuinely cannot use, and that
# is enforced rather than merely written down: checkFamilyBlockerLockstep
# (go/internal/reducer/materialized_edge_family_blocker_shape_test.go:283)
# rejects it for a handler with no IntentWriter, and names ack_barrier or a
# table_lock:<name> the handler really touches as the alternatives. This row
# takes the second, because that is what the cell actually engages.
IFA_FAMILY_BLOCKER_KIND[codeowners_ownership_edges]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[codeowners_ownership_edges]="handler"
# ifa_fault_injection_codeowners_cells.sh:123: (..., "codeowners_ownership")
# -- the FIRST-stage domain (reducer.DomainCodeownersOwnership). The
# SECOND-stage shared-projection domain is the different string
# "codeowners_ownership_edges" (reducer.DomainCodeownersOwnershipEdges, per
# that file's own two-stage-pipeline comment, :35-50) -- do not confuse the
# two if this family ever adds a wait_stage=runner cell.
IFA_FAMILY_WAIT_KEY[codeowners_ownership_edges]="codeowners_ownership"
# Driven in the determinism gate's shared N={1,2,4} cell as of #6160, which
# landed this family's cassette + expected-edge entries in
# ifa_family_fixtures.sh and its drive/assert helpers in
# scripts/lib/ifa_codeowners_live.sh. Before that this row read 0 with a
# "NOT YET WIRED" note, which was accurate when written and is not now --
# the rebase onto #6160 is what changed it.
#
# The fault side is separate and is NOT drive_all_cassettes: #6160 gave this
# family its own scoped baseline plus two recovery cells (cells 19-21), the
# deployable_unit shape, because its edges need a maintenance pass the shared
# cells do not run. shared_cell describes the determinism shared cell only.
IFA_FAMILY_SHARED_CELL[codeowners_ownership_edges]=1
# Dispatch metadata for the determinism drive/assert loop. Signatures follow
# the majority labeled shape -- ifa_codeowners_drive(label, bin_dir, cassette,
# n, log_dir) and ifa_codeowners_assert(label, bin_dir, expected) -- verified
# against scripts/lib/ifa_codeowners_live.sh:15,55, so no irregularity shim is
# needed the way sql_relationships' grandfathered signature requires one.
IFA_FAMILY_DRIVE_FN[codeowners_ownership_edges]="ifa_codeowners_drive"
IFA_FAMILY_ASSERT_FN[codeowners_ownership_edges]="ifa_codeowners_assert"
IFA_FAMILY_CASSETTE_VAR[codeowners_ownership_edges]="codeowners_cassette"
IFA_FAMILY_EXPECTED_VAR[codeowners_ownership_edges]="codeowners_expected_edges"
# The live cell reads codeowners_edge_operation_match
# (scripts/verify-ifa-fault-injection.sh:303), not this field -- this family is
# cell_kind=custom, so the generic dispatcher never calls ifa_family_anchor for
# it. Both strings are byte-exact substrings of the real Cypher statement,
# go/internal/storage/cypher/canonical_codeowners_edges.go:35, but they are not
# the same substring: the live one is the bare prefix
# `MERGE (repo)-[rel:DECLARES_CODEOWNER`, and this row records the full form
# including the property map. The property map is the more precise anchor
# because the codeowners MERGE key is NOT the bare relationship type -- that
# same file's header (:16-28) explains the key intentionally includes pattern
# and source_path so distinct CODEOWNERS rules do not collapse onto one edge.
# Nothing reconciles the two today. If this family is ever flipped to generic
# dispatch, that flip silently changes which string the fault targets, so
# reconcile them in the same change rather than discovering it from a cell that
# fires on the wrong write.
IFA_FAMILY_ANCHOR[codeowners_ownership_edges]="MERGE (repo)-[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]->(team)"
# ack_barrier is not one of the shapes ifa_fault_generic_cells.sh's generic
# dispatcher builds, so custom. Custom families are dispatched by name from
# the gate (ifa_fault_shard_run cell_killworker_codeowners), never through
# that dispatcher -- #6160 wired this family's three cells that way.
IFA_FAMILY_CELL_KIND[codeowners_ownership_edges]="custom"

# Deliberately empty: no ack_barrier-shaped kill cell exists for this family
# yet. The landed cell_killworker_codeowners uses the vacuous
# shared_intent_lock shape this row's blocker_kind explicitly disagrees with
# (see that field's comment above) and MUST NOT be dispatched to here as if
# it satisfied this row's claim. Tracked under #5992.

IFA_FAMILY_HANDLER_GO_FILE[codeowners_ownership_edges]="go/internal/reducer/codeowners_ownership_materialization.go"

IFA_FAMILY_NAMES+=(codeowners_ownership_edges)
