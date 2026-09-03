// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "time"

// These four read models were already exported from package query, but a
// ContentStore double cannot import package query without a cycle -- an
// internal test file in package query imports the double's package, so the
// double importing package query back is rejected outright. They live here so
// the double can name them, and package query keeps an exported alias for
// each, which leaves every existing caller unchanged (#6060, epic #6053).

// RepositoryReadModelSummary is the Postgres read-model fast path for a
// repository's workload names, deployment-platform materialization count, and
// dependency count.
//
// Available false means the read model holds nothing for the repository.
// Callers must fall back to the graph counts rather than read a zero-value
// summary as authoritative -- treating it as truth reports a repository with
// real workloads as having none.
type RepositoryReadModelSummary struct {
	Available       bool
	WorkloadNames   []string
	PlatformCount   int
	PlatformTypes   []string
	DependencyCount int
}

// RepositoryRelationshipReadModel is the Postgres read-model fast path for a
// repository's resolved relationships and derived consumers, hydrated from
// resolved_relationships so a read avoids the incoming-fanout graph traversal.
//
// Available carries the same obligation as on RepositoryReadModelSummary: fall
// back to the graph queries, do not read a zero value as an empty answer.
type RepositoryRelationshipReadModel struct {
	Available     bool
	Relationships []map[string]any
	Consumers     []map[string]any
}

// RepositoryRef is one source-backed repository branch or ref head.
//
// ObservedAt is when the source reported the head; IndexedAt is when Eshu
// stored it. They differ whenever indexing lags, and a freshness check wants
// the first, not the second.
type RepositoryRef struct {
	Name       string
	Kind       string
	HeadSHA    string
	Default    bool
	ObservedAt time.Time
	IndexedAt  time.Time
}

// CatalogWorkloadIdentityEntry is a repository read-model workload handle for
// the console catalog.
type CatalogWorkloadIdentityEntry struct {
	Name     string
	RepoID   string
	RepoName string
}
