#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# uses_vault_policy hand-derived pin (#6228 slice A). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# Kill-less per arbiter verdict Option B on #6228: no remaining cell takes
# a lock, so the hand-derived blocker is none -- recorded faithfully, not
# silently upgraded (same precedent as the sql_relationships pin). Read off
# scripts/lib/ifa_fault_injection_uses_vault_policy_cells.sh directly: the
# baseline and fail-graph-write cells stage a trust-chain reducer, stop it,
# then run the projection reducer with no LOCK TABLE anywhere in the file.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="none"
# NO WAIT (kill-less shape, #8539). Both remaining cells poll no claim wait:
# the trust-chain stage banks read-model facts by polling fact_records (not
# fact_work_items), and the projection stage runs unheld. The shared
# secrets_iam_graph_projection handler wait is owned first-landed by
# authenticates_vault_role (rows/21, committed 88e1b18520); reusing that key
# here would double-count one mechanism run as two per-family proofs
# (TestIfaFamilyRegistryHandlerWaitKeysAreExclusive), and any other key
# would name a wait nothing polls. An empty pin value asserts the registry
# row declares NO wait_stage/wait_key at all -- see
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh.
IFA_FAMILY_PIN_WAIT_STAGE=""
IFA_FAMILY_PIN_WAIT_KEY=""

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
# baseline/failgraphwrite pair in
# scripts/lib/ifa_fault_injection_uses_vault_policy_cells.sh,
# dispatched by name (#6228 slice A, kill-less per Option B). Custom rather
# than generic for the reason recorded in the sibling
# iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
