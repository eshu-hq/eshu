// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"time"
)

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

// RepositoryReadModelSummaryStore is the narrow optional port a ContentStore
// implements to answer read-model summary reads directly. It lives here
// (promoted from root package query for #6060 lane B B3) so the moved
// repository handler family can assert a store against it without importing
// root; root keeps its unexported spelling as a structural twin.
type RepositoryReadModelSummaryStore interface {
	RepositoryReadModelSummary(context.Context, string) (RepositoryReadModelSummary, error)
}

// LoadRepositoryReadModelSummary returns the Postgres read-model fast path
// for repoID when the content store can answer it directly. It lives here
// for the same reason as the port above; root keeps an unexported forwarder
// so its stayers and read-model tripwires compile unchanged.
func LoadRepositoryReadModelSummary(ctx context.Context, content ContentStore, repoID string) *RepositoryReadModelSummary {
	store, ok := content.(RepositoryReadModelSummaryStore)
	if !ok || repoID == "" {
		return nil
	}
	summary, err := store.RepositoryReadModelSummary(ctx, repoID)
	if err != nil || !summary.Available {
		return nil
	}
	return &summary
}

// RepositoryRelationshipReadModelStore is the narrow optional port a
// ContentStore implements to answer relationship read-model reads directly.
// It lives here (promoted from root package query for #6060 lane B B3) so
// the moved repository handler family can assert a store against it without
// importing root; root keeps its unexported spelling as a structural twin.
type RepositoryRelationshipReadModelStore interface {
	RepositoryRelationshipReadModel(context.Context, string) (RepositoryRelationshipReadModel, error)
}

// LoadRepositoryRelationshipReadModel returns resolved relationship truth
// from the Postgres read model when the content store can provide it. It
// lives here for the same reason as the port above; root keeps an
// unexported forwarder so its stayers and read-model tripwires compile
// unchanged.
func LoadRepositoryRelationshipReadModel(ctx context.Context, content ContentStore, repoID string) *RepositoryRelationshipReadModel {
	store, ok := content.(RepositoryRelationshipReadModelStore)
	if !ok || repoID == "" {
		return nil
	}
	readModel, err := store.RepositoryRelationshipReadModel(ctx, repoID)
	if err != nil || !readModel.Available {
		return nil
	}
	return &readModel
}
