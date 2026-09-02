// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/semanticsearch"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// SemanticSearchHandler exposes bounded curated search-document retrieval. The
// implementation moved to internal/query/semanticsearch (#6060); this alias
// preserves the compatibility surface for cmd/api, cmd/mcp-server, and root's
// own tests, none of which import the family package directly. The alias
// carries the handler's exported methods (Mount) and fields unchanged; it
// cannot forward unexported helpers, and none of those cross this boundary.
type SemanticSearchHandler = semanticsearch.SemanticSearchHandler

// SemanticSearchIndexStore searches a persisted curated search-document index
// for one repository-scoped corpus. See semanticsearch.SemanticSearchIndexStore.
type SemanticSearchIndexStore = semanticsearch.SemanticSearchIndexStore

// SemanticSearchIndexQuery is one bounded retrieval against the curated
// search-document index. See semanticsearch.SemanticSearchIndexQuery.
type SemanticSearchIndexQuery = semanticsearch.SemanticSearchIndexQuery

// SemanticSearchIndexResult is what a store returns for one index query. See
// semanticsearch.SemanticSearchIndexResult.
type SemanticSearchIndexResult = semanticsearch.SemanticSearchIndexResult

// SemanticSearchHybridStore serves the vector-backed semantic and hybrid
// retrieval modes. See semanticsearch.SemanticSearchHybridStore.
type SemanticSearchHybridStore = semanticsearch.SemanticSearchHybridStore

// SemanticSearchScopeResolver resolves an authorized repository identity to
// its active ingestion scope. See semanticsearch.SemanticSearchScopeResolver.
type SemanticSearchScopeResolver = semanticsearch.SemanticSearchScopeResolver

// SemanticSearchVectorReadyReader reports the search-vector build sweep's
// watermark. See semanticsearch.SemanticSearchVectorReadyReader.
type SemanticSearchVectorReadyReader = semanticsearch.SemanticSearchVectorReadyReader

// SemanticSearchSnapshot is a cached bounded corpus snapshot. See
// semanticsearch.SemanticSearchSnapshot.
type SemanticSearchSnapshot = semanticsearch.SemanticSearchSnapshot

// SemanticSearchSnapshotRequest addresses one corpus snapshot. See
// semanticsearch.SemanticSearchSnapshotRequest.
type SemanticSearchSnapshotRequest = semanticsearch.SemanticSearchSnapshotRequest

// SemanticSearchSnapshotStore loads cached corpus snapshots. See
// semanticsearch.SemanticSearchSnapshotStore.
type SemanticSearchSnapshotStore = semanticsearch.SemanticSearchSnapshotStore

// PersistedLocalSemanticSearchHybrid serves hybrid retrieval from persisted
// vectors. See semanticsearch.PersistedLocalSemanticSearchHybrid.
type PersistedLocalSemanticSearchHybrid = semanticsearch.PersistedLocalSemanticSearchHybrid

// PersistedLocalSemanticSearchHybridConfig bounds the persisted-vector hybrid
// backend. See semanticsearch.PersistedLocalSemanticSearchHybridConfig.
type PersistedLocalSemanticSearchHybridConfig = semanticsearch.PersistedLocalSemanticSearchHybridConfig

// PostgresSemanticSearchScopeResolver resolves repository scope from Postgres.
// See semanticsearch.PostgresSemanticSearchScopeResolver.
type PostgresSemanticSearchScopeResolver = semanticsearch.PostgresSemanticSearchScopeResolver

// PostgresSemanticSearchSnapshotStore loads corpus snapshots from Postgres.
// See semanticsearch.PostgresSemanticSearchSnapshotStore.
type PostgresSemanticSearchSnapshotStore = semanticsearch.PostgresSemanticSearchSnapshotStore

// SearchVectorBuildIdentity names the build sweep whose watermark is read. See
// semanticsearch.SearchVectorBuildIdentity.
type SearchVectorBuildIdentity = semanticsearch.SearchVectorBuildIdentity

// DefaultPersistedLocalSemanticSearchHybridConfig returns the default bounds
// for the persisted-vector hybrid backend. cmd/api calls this through package
// query rather than semanticsearch directly (#6060); it forwards unchanged.
func DefaultPersistedLocalSemanticSearchHybridConfig() PersistedLocalSemanticSearchHybridConfig {
	return semanticsearch.DefaultPersistedLocalSemanticSearchHybridConfig()
}

// NewCachedPersistedLocalSemanticSearchHybrid constructs the persisted-vector
// hybrid backend with a corpus snapshot cache. Forwards unchanged to
// semanticsearch.NewCachedPersistedLocalSemanticSearchHybrid.
func NewCachedPersistedLocalSemanticSearchHybrid(
	documents semanticsearch.SemanticSearchDocumentStore,
	metadata semanticsearch.SemanticSearchVectorMetadataStore,
	values semanticsearch.SemanticSearchVectorValueStore,
	snapshots SemanticSearchSnapshotStore,
	embedder searchhybrid.Embedder,
	config PersistedLocalSemanticSearchHybridConfig,
) *PersistedLocalSemanticSearchHybrid {
	return semanticsearch.NewCachedPersistedLocalSemanticSearchHybrid(
		documents, metadata, values, snapshots, embedder, config,
	)
}

// NewPostgresSemanticSearchIndexStore constructs the Postgres-backed curated
// search-document index store. Forwards unchanged to
// semanticsearch.NewPostgresSemanticSearchIndexStore.
func NewPostgresSemanticSearchIndexStore(db *sql.DB) semanticsearch.PostgresSemanticSearchIndexStore {
	return semanticsearch.NewPostgresSemanticSearchIndexStore(db)
}

// NewPostgresSemanticSearchScopeResolver constructs the Postgres-backed
// repository-to-scope resolver. Forwards unchanged to
// semanticsearch.NewPostgresSemanticSearchScopeResolver.
func NewPostgresSemanticSearchScopeResolver(db pgstatus.Queryer) PostgresSemanticSearchScopeResolver {
	return semanticsearch.NewPostgresSemanticSearchScopeResolver(db)
}

// NewPostgresSemanticSearchSnapshotStore constructs the Postgres-backed corpus
// snapshot store. Forwards unchanged to
// semanticsearch.NewPostgresSemanticSearchSnapshotStore.
func NewPostgresSemanticSearchSnapshotStore(db pgstatus.Queryer) PostgresSemanticSearchSnapshotStore {
	return semanticsearch.NewPostgresSemanticSearchSnapshotStore(db)
}

// NewPostgresSearchVectorReadyStore constructs the Postgres-backed
// search-vector-ready watermark reader. Forwards unchanged to
// semanticsearch.NewPostgresSearchVectorReadyStore.
func NewPostgresSearchVectorReadyStore(
	db semanticsearch.SearchVectorReadyQueryer,
	identity SearchVectorBuildIdentity,
) semanticsearch.PostgresSearchVectorReadyStore {
	return semanticsearch.NewPostgresSearchVectorReadyStore(db, identity)
}
