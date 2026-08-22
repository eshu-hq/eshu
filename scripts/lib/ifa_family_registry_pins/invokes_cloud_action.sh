#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# invokes_cloud_action hand-derived pin (#5997). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that
# file's header before touching this one, and read
# scripts/lib/ifa_family_registry_pins/handles_route.sh first: the
# blocker_kind/cell_kind reasoning is identical across all three sibling
# rows (handles_route #5995, runs_in #6000, invokes_cloud_action #5997) and
# is not repeated in full here.

# runner_lease_hold: the custom kill cell blocks the production
# ClaimPartitionLease advisory key for DomainInvokesCloudAction. See
# handles_route.sh.
IFA_FAMILY_PIN_BLOCKER_KIND="runner_lease_hold"
# wait_stage=runner: this family's rows are tagged
# ProjectionDomain=DomainInvokesCloudAction="invokes_cloud_action"
# (go/internal/reducer/shared_projection.go:44,
# go/internal/reducer/invokes_cloud_action_intents.go:207) -- the
# shared_projection_intents.projection_domain column.
IFA_FAMILY_PIN_WAIT_STAGE="runner"
IFA_FAMILY_PIN_WAIT_KEY="invokes_cloud_action"

# go/internal/storage/cypher/canonical_invokes_cloud_action_edges.go:22.
# Single relationship type, so this anchor covers the family's whole write
# surface. The CloudAction node id ("cloud-action:" + action) is created
# inline by this same MERGE.
IFA_FAMILY_PIN_ANCHOR="MERGE (func)-[rel:INVOKES_CLOUD_ACTION]->(action)"
# shared_cell: a plain reducer family needing no maintenance pass, driven in
# the determinism gate's shared N={1,2,4} cell via a drive_fn shared with
# its two sibling rows.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind=custom: its baseline, graph-write-failure, and runner-lease kill
# cells are hand-written and dispatched by name.
IFA_FAMILY_PIN_CELL_KIND="custom"
