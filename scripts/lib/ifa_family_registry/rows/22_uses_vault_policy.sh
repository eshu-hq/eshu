#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# uses_vault_policy row (#6228 slice A). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read rows/13_iam_instance_profile_role.sh's header
# first -- it explains what a DIRECT-materialization family is and which
# blocker kinds are unavailable to one.
#
# FAULT CELLS LANDED (#6228 slice A full build, plus the per-statement hold
# kill cell from the #8539 seam lane). scripts/verify-ifa-fault-injection.sh
# drives and asserts this row through
# scripts/lib/ifa_fault_injection_uses_vault_policy_cells.sh
# (baseline/failgraphwrite/kill trio, dispatched in shard order with a
# co-location atomic group). cell_kind=custom below records the hand-written
# dispatch; specs/ci-gates.v1.yaml gives this family ifa-fault-injection
# triggers so an edit to its cassette or its writer re-runs the matrix that
# observes it.
#
# PER-STATEMENT HOLD KILL CELL (seam (2) of the #8539 arbiter ruling
# 2026-10-21; the per-family projection-domain split alternative was
# rejected). The family's edges are written by the
# DomainSecretsIAMGraphProjection handler -- the SAME handler that writes
# authenticates_vault_role's edges -- and a handler-stage kill of it is
# already claimed by rows/21: authenticates_vault_role landed first
# (committed 88e1b18520) and owns the secrets_iam_graph_projection handler
# wait by first-landed tie-break. So this row does NOT reuse that
# handler-stage wait: it declares wait_stage=statement with the same
# secrets_iam_graph_projection wait_key as rendezvous only (the claimed
# projection work item), while per-family exclusivity is judged on this
# row's own IFA_FAMILY_ANCHOR -- "a blocker that engages a write only it
# performs". The hold (hold-statement-before-delegation, matched on that
# anchor) lands post-retract, post-prior-edge-write, worker dead mid-handler:
# a fault point the authenticates table_lock:fact_records kill (pre-retract,
# at the fact load) never reaches.
#
# TWO-STAGE CHAIN, one reducer with the flag on for that cell only. The
# family's edges are written by the DomainSecretsIAMGraphProjection handler
# (SecretsIAMUsesVaultPolicyEdgeUpsertCypher in
# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go), which
# reads reducer-owned read-model facts -- so the live cells run the
# trust-chain stage first (trust-chain-scoped reducer, flag on), then the
# projection stage (projection-scoped reducer, flag on). The projection
# handler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/secretsiam/secrets_iam_graph_projection.go:81) and
# Handle refuses to run without it (:101) before passing it to
# factload.LoadFactsForKinds (:121). The handler reads fact_records AFTER
# claiming its work item, so the projection stage MUST wait for the
# trust-chain stage's read-model facts to commit before it starts, or the
# projection claims an empty generation, succeeds vacuously, and never
# re-runs. The projection claim is NOT claim-time-readiness-gated (no
# readiness row names this domain), so no cell needs a prereq-scoped
# reducer. The projection intent itself is created by the trust-chain
# handler when those read models commit (GraphProjectionFollowUpWriter),
# not by the projector. Separately, the handler-level endpoint-readiness
# gate (PresenceLookup, wired whenever the flag is on) defers the
# projection until the cross-scope endpoints commit: the fixture carries
# its own pod-template and aws_resource observations for the exact lanes'
# workloads and IAM roles because no other scope commits them.
#
# The flag (ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED) is a live-cell
# stack env only, never a deployment activation: the wiring file
# (go/cmd/reducer/secrets_iam_graph_wiring.go) reserves enabling it for a
# target-bound activation record, and a CI cell stack is not a deployment.
# The assumes_iam_role waiver in
# specs/ifa-materialized-edge-coverage-direct.v1.yaml prescribes exactly this
# shape (flag on for that cell only, collector-only cassette).
#
# blocker_kind=statement_hold: the kill cell holds the family's own MERGE
# through the hold decorator (see IFA_FAMILY_ANCHOR below), not a lock.
# wait_stage=statement names the same secrets_iam_graph_projection domain the
# handler-stage wait uses, but ONLY as the marker-plus-claim rendezvous --
# handler-stage exclusivity still belongs to rows/21 by first-landed
# tie-break, and statement-stage exclusivity is judged on the anchor
# (go/internal/reducer/ifa_family_registry_statement_hold_coherence_test.go).
IFA_FAMILY_BLOCKER_KIND[uses_vault_policy]="statement_hold"
IFA_FAMILY_WAIT_STAGE[uses_vault_policy]="statement"
# The fact_work_items.domain the projection handler fans this family out
# under: the projection domain itself
# (reducercontract.DomainSecretsIAMGraphProjection in
# go/internal/reducer/contract/domain.go). Same value as rows/21's handler
# wait, deliberately: for a statement-stage row the key is rendezvous only.
IFA_FAMILY_WAIT_KEY[uses_vault_policy]="secrets_iam_graph_projection"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[uses_vault_policy]=1

IFA_FAMILY_DRIVE_FN[uses_vault_policy]="ifa_uses_vault_policy_drive"
IFA_FAMILY_ASSERT_FN[uses_vault_policy]="ifa_uses_vault_policy_assert"
IFA_FAMILY_CASSETTE_VAR[uses_vault_policy]="uses_vault_policy_cassette"
IFA_FAMILY_EXPECTED_VAR[uses_vault_policy]="uses_vault_policy_expected_edges"

# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:108, which
# reads `MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)` in
# source. The anchor is matched against EXECUTED statement text and the token
# is static in the template (no %s to fill), so the anchor carries the
# literal. Pinning a `%s` form here would match no executed statement and a
# scripted graph-write fault would never fire -- a green cell that tested
# nothing.
#
# It is NOT the bare USES_VAULT_POLICY fragment: the full MERGE literal pins
# the exact statement (Vault-role endpoint to Vault-policy endpoint). The
# type is read off the template, never derived from the port or family name.
IFA_FAMILY_ANCHOR[uses_vault_policy]="MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[uses_vault_policy]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[uses_vault_policy]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[uses_vault_policy]="0"

IFA_FAMILY_HANDLER_GO_FILE[uses_vault_policy]="go/internal/reducer/secretsiam/secrets_iam_graph_projection.go"

IFA_FAMILY_NAMES+=(uses_vault_policy)
