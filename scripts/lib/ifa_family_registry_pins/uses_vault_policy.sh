#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# uses_vault_policy hand-derived pin (#6228 slice A). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# Per-statement hold (seam (2) of the #8539 arbiter ruling 2026-10-21).
# Read off scripts/lib/ifa_fault_injection_uses_vault_policy_cells.sh
# directly: the kill cell engages the hold-statement-before-delegation fault
# on this family's own USES_VAULT_POLICY MERGE (cell_kill_uses_vault_policy),
# staged behind the same trust-chain reducer + read-model banking as the
# baseline and fail-graph-write cells.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take; the hold needs none, engaging the graph
# executor rather than a lock.
IFA_FAMILY_PIN_BLOCKER_KIND="statement_hold"
# STATEMENT-stage rendezvous (#8539 seam). The kill cell polls
# fact_work_items for the claimed secrets_iam_graph_projection row (same
# queue the handler stage polls), then the hold marker, before kill -9.
# The shared secrets_iam_graph_projection handler wait stays owned
# first-landed by authenticates_vault_role (rows/21, committed 88e1b18520):
# this key is rendezvous only, and statement-stage exclusivity is judged on
# the anchor, not the key
# (go/internal/reducer/ifa_family_registry_statement_hold_coherence_test.go).
IFA_FAMILY_PIN_WAIT_STAGE="statement"
IFA_FAMILY_PIN_WAIT_KEY="secrets_iam_graph_projection"

# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:108 reads
# `MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)`. The anchor
# is matched against EXECUTED statement text and the token is static in the
# template (no %s to fill), so the anchor carries the literal. A `%s` form
# here would match no executed statement, the scripted graph-write fault
# would never fire, and the cell would report green having tested nothing.
#
# NOT the bare USES_VAULT_POLICY fragment -- the full MERGE literal pins the
# exact statement (Vault-role endpoint to Vault-policy endpoint). The type
# is read off the template, never derived from the port or family name.
IFA_FAMILY_PIN_ANCHOR="MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom
# baseline/failgraphwrite/kill trio in
# scripts/lib/ifa_fault_injection_uses_vault_policy_cells.sh,
# dispatched by name (#6228 slice A + #8539 seam). Custom rather
# than generic for the reason recorded in the sibling
# iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
