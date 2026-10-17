// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package writer owns the canonical Cypher edge writer: EdgeWriter applies
// domain edge upserts and retracts through a sourcecypher.Executor in
// whole-scope, delta, and per-file narrowed modes with batching,
// nil-fencing, and fail-closed whole-scope narrowing.
//
// WriteEdges fans a batch of reducer projection rows out to the domain
// label families (code-call, documentation, inheritance, rationale,
// shell-exec, SQL, workload, handles-route, runs-in, deployable-unit,
// repo-dependency, codeowners, submodule, invokes-cloud-action, and the
// cross-repo/retract roles); RetractEdges reaps the same families by scope.
// Every Cypher statement template lives beside the writer that emits it and
// moves byte-identically with it.
//
// VerifyDeployableUnitEdges reads admitted CORRELATES_DEPLOYABLE_UNIT edges
// back after WriteEdges (#8283) with one single-pattern probe per distinct
// pair, and classifies a miss as an absent endpoint (non-counting
// shared_edge_target_not_ready) or a lost write with both endpoints present
// (counting deployable_unit_edge_write_unverified).
//
// The BatchCanonical* and Retract* statement constants shared with the
// canonical writers stay exported because the canonical family (still in the
// cypher root until its own #6694 leaf) and the edge/materialized sibling
// read through them. Shared nothing else leaves this package: every helper
// is family-private. This package must not import the sibling materialized
// package; both leaves import only the parent cypher package.
package writer
