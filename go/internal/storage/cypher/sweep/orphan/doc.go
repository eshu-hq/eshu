// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package orphan owns the graph orphan sweep: OrphanSweepStore counts,
// marks, and deletes disconnected graph nodes through backend-neutral Cypher
// seams, computing orphan status as a Go-side anti-join between two bounded
// reads (candidates and connected keys) and applying only key-anchored
// writes for the exact key sets the anti-join produced.
//
// The store never relies on a relationship-existence predicate: every such
// predicate is mis-evaluated on the pinned NornicDB backends. See
// docs/public/reference/nornicdb-pitfalls.md and store.go.
//
// This is a Pattern-B leaf: it imports the parent cypher package for the
// Executor and Statement types (edge/writer precedent). The parent and
// white-box parent tests must never import this package.
package orphan
