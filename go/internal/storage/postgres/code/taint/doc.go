// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package taintstore records the durable projected-edge and projected-node
// ledgers backing #4893's value-flow retraction path: the source Function
// uid of every projected TAINT_FLOWS_TO edge (CodeInterprocProjectedEdgeStore)
// and the node uid of every projected CodeTaintEvidence node
// (CodeTaintEvidenceProjectedNodeStore).
//
// Both ledgers are written before their corresponding graph write, so each
// is always a superset of the graph state it tracks: over-inclusion is
// harmless because the anchored Cypher retract WHERE still filters by scope
// and generation, but under-inclusion would orphan a graph edge or node that
// retraction can no longer find. That invariant is why RecordProjectedEdges
// and RecordProjectedNodes must run to completion before the graph write
// they precede, not after.
//
// Both stores follow the same shape: an idempotent batched upsert
// (RecordProjectedEdges / RecordProjectedNodes) that de-duplicates uids
// within a batch and skips blank ones, a SchemaSQL() constant mirrored
// byte-for-byte by its migration file, ORDER BY-bounded SELECT DISTINCT
// enumeration for the current and stale generations, and generation-scoped
// bounded DELETE pruning. See go/internal/reducer/AGENTS.md (#4893) for how
// the reducer's backfillers and stale-cleanup runner drive these ledgers.
//
// This package must not import the parent internal/storage/postgres
// package: that import direction would create a cycle, since root code
// constructs these stores.
package taintstore
