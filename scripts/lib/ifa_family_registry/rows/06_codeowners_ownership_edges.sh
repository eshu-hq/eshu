#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# codeowners_ownership_edges row. See ../../ifa_family_registry.sh for the
# schema and every array declaration this file assigns into.

# CORRECTED from the landed cell. ifa_fault_injection_codeowners_cells.sh's
# cell_killworker_codeowners (:65-92) calls ifa_codeowners_start_intent_lock,
# which locks shared_projection_intents -- but `rg IntentWriter
# go/internal/reducer/codeowners_ownership_materialization.go` returns
# nothing: this handler has NO IntentWriter, so that lock can never engage
# (the exact "lock #1" defect ifa_fault_injection_deployable_unit_lock.sh:
# 81-86 already names for a different family). The landed cell is tracked
# separately (#5992); this row records the kind the handler's real write
# path actually supports -- an ACK-barrier shape, like documentation_edges
# -- not the vacuous kind the landed cell happens to use today. Do NOT edit
# ifa_fault_injection_codeowners_cells.sh to match this row; that is out of
# scope here.
IFA_FAMILY_BLOCKER_KIND[codeowners_ownership_edges]="ack_barrier"
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
# No shell operation_match var exists for this family yet (NOT YET WIRED,
# see blocker_kind's comment above). Read directly from the real Cypher
# statement, go/internal/storage/cypher/canonical_codeowners_edges.go:35.
# The property map is included because the codeowners MERGE key is NOT the
# bare relationship type -- that same file's header (:16-28) explains the
# key intentionally includes pattern and source_path so distinct CODEOWNERS
# rules do not collapse onto one edge; a substring match on the bare
# ]->(team) alone would not be anchored to a specific rule.
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
