#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# authenticates_vault_role row (#6228 slice A). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read rows/13_iam_instance_profile_role.sh's header
# first -- it explains what a DIRECT-materialization family is and which
# blocker kinds are unavailable to one.
#
# FAULT CELLS LANDED (#6228 slice A full build). scripts/verify-ifa-fault-injection.sh
# drives and asserts this row through
# scripts/lib/ifa_fault_injection_authenticates_vault_role_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with a
# co-location atomic group). cell_kind=custom below records the hand-written
# dispatch; specs/ci-gates.v1.yaml gives this family ifa-fault-injection
# triggers so an edit to its cassette or its writer re-runs the matrix that
# observes it.
#
# TWO-STAGE CHAIN, one reducer with the flag on for that cell only. The
# family's edges are written by the DomainSecretsIAMGraphProjection handler
# (SecretsIAMAuthenticatesVaultRoleEdgeUpsertCypher in
# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go), which
# reads reducer-owned read-model facts -- so the live cells run the
# trust-chain stage first (trust-chain-scoped reducer, flag on), then the
# projection stage (projection-scoped reducer, flag on). The projection
# handler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/secretsiam/secrets_iam_graph_projection.go:81) and
# Handle refuses to run without it (:101) before passing it to
# factload.LoadFactsForKinds (:121). The handler reads fact_records AFTER
# claiming its work item, so an ACCESS EXCLUSIVE lock on that table holds it
# genuinely in flight -- the blocker_kind this row's fault cell takes. Same
# table and same reasoning as rows/13; see that row for why fact_work_items
# is the wrong target. The projection claim is NOT claim-time-readiness-gated
# (no readiness row names this domain), so the kill cell needs no
# prereq-scoped reducer -- but it MUST wait for the trust-chain stage's
# read-model facts to commit before the lock goes on, or the projection
# claims an empty generation, succeeds vacuously, and never re-runs. The
# projection intent itself is created by the trust-chain handler when those
# read models commit (GraphProjectionFollowUpWriter), not by the projector,
# so the kill cell waits for the row only after the trust-chain stage.
# Separately, the handler-level endpoint-readiness gate (PresenceLookup,
# wired whenever the flag is on) defers the projection until the cross-scope
# endpoints commit: the fixture carries its own pod-template and aws_resource
# observations for the exact lanes' workloads and IAM roles because no other
# scope commits them.
#
# The flag (ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED) is a live-cell
# stack env only, never a deployment activation: the wiring file
# (go/cmd/reducer/secrets_iam_graph_wiring.go) reserves enabling it for a
# target-bound activation record, and a CI cell stack is not a deployment.
# The assumes_iam_role waiver in
# specs/ifa-materialized-edge-coverage-direct.v1.yaml prescribes exactly this
# shape (flag on for that cell only, collector-only cassette).
IFA_FAMILY_BLOCKER_KIND[authenticates_vault_role]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[authenticates_vault_role]="handler"
# The fact_work_items.domain the projector fans this family out under: the
# projection domain itself
# (reducercontract.DomainSecretsIAMGraphProjection in
# go/internal/reducer/contract/domain.go:133). The trust-chain stage runs
# first under secrets_iam_trust_chain, but the fault cell holds the
# PROJECTION handler in flight, so the wait key names the projection domain.
IFA_FAMILY_WAIT_KEY[authenticates_vault_role]="secrets_iam_graph_projection"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[authenticates_vault_role]=1

IFA_FAMILY_DRIVE_FN[authenticates_vault_role]="ifa_authenticates_vault_role_drive"
IFA_FAMILY_ASSERT_FN[authenticates_vault_role]="ifa_authenticates_vault_role_assert"
IFA_FAMILY_CASSETTE_VAR[authenticates_vault_role]="authenticates_vault_role_cassette"
IFA_FAMILY_EXPECTED_VAR[authenticates_vault_role]="authenticates_vault_role_expected_edges"
# The shared N-cell also holds the uses-vault-policy scope, whose exact chain
# takes the Vault hop and writes this same edge type: the loop must assert
# the four-edge combined file here while fault cells (fresh per-family DB)
# keep the two-edge EXPECTED_VAR above.
IFA_FAMILY_SHARED_EXPECTED_VAR[authenticates_vault_role]="authenticates_vault_role_live_expected_edges"

# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:97, which
# reads `MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v)` in
# source. The anchor is matched against EXECUTED statement text and the token
# is static in the template (no %s to fill), so the anchor carries the
# literal. Pinning a `%s` form here would match no executed statement and a
# scripted graph-write fault would never fire -- a green cell that tested
# nothing.
#
# It is NOT AUTHENTICATES_VAULT_ROLE: that string is the port suffix, naming
# metadata carried beside the query that never reaches the graph. The type is
# read off the template, never derived from the port or family name.
IFA_FAMILY_ANCHOR[authenticates_vault_role]="MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[authenticates_vault_role]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[authenticates_vault_role]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[authenticates_vault_role]="0"

IFA_FAMILY_HANDLER_GO_FILE[authenticates_vault_role]="go/internal/reducer/secretsiam/secrets_iam_graph_projection.go"

IFA_FAMILY_NAMES+=(authenticates_vault_role)
