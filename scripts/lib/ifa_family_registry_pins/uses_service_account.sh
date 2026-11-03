#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# uses_service_account hand-derived pin (#6228 uses_service_account live leaf). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# Per-statement hold (seam (2) of the #8539 arbiter ruling 2026-10-21).
# Read off scripts/lib/ifa_fault_injection_uses_service_account_cells.sh
# directly: the kill cell engages the hold-statement-before-delegation fault
# on this family's own USES_SERVICE_ACCOUNT MERGE (cell_kill_uses_service_account),
# staged behind the same trust-chain reducer + read-model banking as the
# baseline and fail-graph-write cells.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take; the hold needs none, engaging the graph
# executor rather than a lock.
IFA_FAMILY_PIN_BLOCKER_KIND="statement_hold"
# STATEMENT-stage rendezvous (#8539 seam). The kill cell polls
# fact_work_items for the claimed secrets_iam_graph_projection row (same
# queue the handler stage polls), plus the hold marker, before kill -9.
# The shared secrets_iam_graph_projection handler wait stays owned
# first-landed by authenticates_vault_role (rows/21): this key is
# rendezvous only, and statement-stage exclusivity is judged on the anchor,
# not the key
# (go/internal/reducer/ifa_family_registry_statement_hold_coherence_test.go).
IFA_FAMILY_PIN_WAIT_STAGE="statement"
IFA_FAMILY_PIN_WAIT_KEY="secrets_iam_graph_projection"

# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:74 reads
# `MERGE (w)-[rel:SECRETS_IAM_USES_SERVICE_ACCOUNT]->(s)`. The anchor
# is matched against EXECUTED statement text and the token is static in the
# template (no %s to fill), so the anchor carries the literal. A `%s` form
# here would match no executed statement, the scripted graph-write fault
# would never fire, and the cell would report green having tested nothing.
#
# NOT the bare USES_SERVICE_ACCOUNT fragment -- the full MERGE literal pins the
# exact statement (KubernetesWorkload endpoint to ServiceAccount endpoint). The
# type is read off the template, never derived from the port or family name.
IFA_FAMILY_PIN_ANCHOR="MERGE (w)-[rel:SECRETS_IAM_USES_SERVICE_ACCOUNT]->(s)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom
# baseline/failgraphwrite/kill trio in
# scripts/lib/ifa_fault_injection_uses_service_account_cells.sh,
# dispatched by name (#6228 uses_service_account live leaf + #8539 seam). Custom rather
# than generic for the reason recorded in the sibling
# iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
