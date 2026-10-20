// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package entity owns the semantic-entity Cypher writer: SemanticEntityWriter
// applies reducer semantic rows (Annotation, Typedef, TypeAlias,
// TypeAnnotation, Component, Module, ImplBlock, Protocol,
// ProtocolImplementation, Variable, and semantic Function nodes) through a
// sourcecypher.Executor in whole-scope, delta, and per-file narrowed modes,
// with batching, label-scoped retracts, and fail-closed delta narrowing.
//
// The writer and its Cypher templates live here; the parent cypher package
// owns the Executor/Statement seam this package builds on. The parent never
// imports this package — the dependency runs one way, leaf to parent — so
// root callers reach the writer through this import path, never the reverse.
//
// WithGenerationCheck (writer_fence.go) arms an optional per-group
// generation fence: every dispatched group, including the first and
// retract-only writes, probes the write's stamped generation before
// committing, and a superseded generation aborts with a partial result
// instead of landing stale rows. A nil check keeps the legacy unfenced path.
package entity
