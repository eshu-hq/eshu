#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# authenticates_vault_role hand-derived pin (#6228 slice A). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/secretsiam/secrets_iam_graph_projection.go
# declares `FactLoader factload.FactLoader` as a struct field at :81, Handle
# rejects a nil one at :101, and passes it to factload.LoadFactsForKinds at
# :121. An ACCESS EXCLUSIVE lock on fact_records therefore blocks a read
# this handler really performs -- the blocker_kind this family's fault
# cell takes.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under: the
# projection domain itself
# (reducercontract.DomainSecretsIAMGraphProjection in
# go/internal/reducer/contract/domain.go:133). The trust-chain stage runs
# first under secrets_iam_trust_chain, but the fault cell holds the
# PROJECTION handler in flight, so the wait key names the projection domain.
IFA_FAMILY_PIN_WAIT_KEY="secrets_iam_graph_projection"

# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:97 reads
# `MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v)`. The anchor
# is matched against EXECUTED statement text and the token is static in the
# template (no %s to fill), so the anchor carries the literal. A `%s` form
# here would match no executed statement, the scripted graph-write fault
# would never fire, and the cell would report green having tested nothing.
#
# NOT AUTHENTICATES_VAULT_ROLE -- that string is the port suffix, naming
# metadata carried beside the query that never reaches the graph. The type
# is read off the template, never derived from the port or family name.
IFA_FAMILY_PIN_ANCHOR="MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_authenticates_vault_role_cells.sh,
# dispatched by name (#6228 slice A). Custom rather than generic for the
# reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
