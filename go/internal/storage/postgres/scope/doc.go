// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scopestore holds the Postgres storage helpers that derive stored
// values from an ingestion scope. It is the `scope/` leaf of the
// storage/postgres split (#6693).
//
// SourceKey computes the ingestion_scopes.source_key column: the trimmed
// Metadata["source_key"] when set, otherwise the scope ID. The ingestion
// commit writes it, and the deferred-maintenance lock falls back to it for a
// scope with no partition key, so both call this one function.
//
// RepoIDFromScopeID derives the lowercased repo_id from a
// git-repository-scope:<repo_id> scope ID and returns "" for every other scope
// shape; the deferred relationship backfill uses it only as a performance
// hint, never as a correctness input.
//
// Both functions are pure: they read no rows and have no side effects. This
// package must not import the parent postgres package. Later #6693 moves
// nest cross-scope completion under it as scope/completion.
package scopestore
