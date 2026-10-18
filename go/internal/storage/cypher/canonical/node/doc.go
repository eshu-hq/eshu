// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package node owns the canonical node-write support data for the storage
// Cypher writer: the node-phase Cypher templates, the entity-label sets the
// retract phase scans, the entity-metadata normalization helpers, and the
// canonical code-target existence checker.
//
// The package holds no writer, executor, or statement type. The parent cypher
// package owns CanonicalNodeWriter, the Statement wrap, and phase dispatch,
// and qualifies every symbol it uses (node.<Name>). This leaf never imports
// its parent, so the dependency runs one way: parent -> node.
package node
