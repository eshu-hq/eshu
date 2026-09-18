// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	semanticSearchScopeStoreName          = "semantic_search_scope"
	semanticSearchSnapshotStoreName       = "semantic_search_snapshot"
	semanticSearchVectorMetadataStoreName = "semantic_search_vector_metadata"
	semanticSearchVectorValueStoreName    = "semantic_search_vector_values"
)

type instrumentedSemanticSearchScopeResolver struct {
	resolver query.PostgresSemanticSearchScopeResolver
	database *pgstatus.InstrumentedDB
}

func (r instrumentedSemanticSearchScopeResolver) ResolveSemanticSearchScope(
	ctx context.Context,
	repoID string,
) (string, error) {
	return r.resolver.ResolveSemanticSearchScope(ctx, repoID)
}

func (r instrumentedSemanticSearchScopeResolver) ResolveSemanticSearchRepositoryForScope(
	ctx context.Context,
	scopeID string,
) (string, error) {
	return r.resolver.ResolveSemanticSearchRepositoryForScope(ctx, scopeID)
}

func newInstrumentedSemanticSearchScopeResolver(
	database *sql.DB,
	instruments *telemetry.Instruments,
) instrumentedSemanticSearchScopeResolver {
	instrumentedDB := newInstrumentedPostgresStore(
		pgstatus.SQLDB{DB: database},
		"mcp-server",
		semanticSearchScopeStoreName,
		instruments,
	)
	return instrumentedSemanticSearchScopeResolver{
		resolver: query.NewPostgresSemanticSearchScopeResolver(instrumentedDB),
		database: instrumentedDB,
	}
}

type instrumentedSemanticSearchVectorMetadataStore struct {
	store    pgstatus.EshuSearchVectorMetadataStore
	database *pgstatus.InstrumentedDB
}

func (s instrumentedSemanticSearchVectorMetadataStore) ListActive(
	ctx context.Context,
	filter pgstatus.EshuSearchVectorMetadataFilter,
) ([]pgstatus.EshuSearchVectorMetadata, error) {
	return s.store.ListActive(ctx, filter)
}

type instrumentedSemanticSearchVectorValueStore struct {
	store    pgstatus.EshuSearchVectorValueStore
	database *pgstatus.InstrumentedDB
}

type instrumentedSemanticSearchSnapshotStore struct {
	store    query.PostgresSemanticSearchSnapshotStore
	database *pgstatus.InstrumentedDB
}

func (s instrumentedSemanticSearchSnapshotStore) Load(
	ctx context.Context,
	request query.SemanticSearchSnapshotRequest,
) (query.SemanticSearchSnapshot, error) {
	return s.store.Load(ctx, request)
}

func (s instrumentedSemanticSearchVectorValueStore) ListActive(
	ctx context.Context,
	filter pgstatus.EshuSearchVectorValueFilter,
) ([]pgstatus.EshuSearchVectorValue, error) {
	return s.store.ListActive(ctx, filter)
}

func newInstrumentedPostgresStore(
	inner db.ExecQueryer,
	tracerName string,
	storeName string,
	instruments *telemetry.Instruments,
) *pgstatus.InstrumentedDB {
	return &pgstatus.InstrumentedDB{
		Inner:       inner,
		Tracer:      otel.Tracer(tracerName),
		Instruments: instruments,
		StoreName:   storeName,
	}
}

// newCodeHybridRanker builds the optional find_code hybrid re-ranker. It is
// gated only on whether semantic search is enabled; it deliberately does NOT
// thread the runtime's semantic-search embedder, because that embedder may be a
// governed provider that POSTs text to an external endpoint. The ranker owns a
// process-local deterministic embedder so request source snippets never egress
// on the find_code path. When semantic search is disabled the ranker is nil and
// find_code keeps its lexical content order.
func newCodeHybridRanker(config searchembedruntime.Config) codemodel.CodeResultReranker {
	if !config.Enabled {
		return nil
	}
	return codemodel.NewCodeHybridRanker(true)
}

// newContentHybridRanker builds the optional search_entity_content /
// search_file_content hybrid re-ranker. Like newCodeHybridRanker it is gated only
// on whether semantic search is enabled and owns a process-local deterministic
// embedder, so request source snippets never egress on the content-search path.
// When semantic search is disabled the ranker is nil and the content-search
// tools keep their lexical content order.
func newContentHybridRanker(config searchembedruntime.Config) query.ContentResultReranker {
	if !config.Enabled {
		return nil
	}
	return query.NewContentHybridRanker(true)
}

func newSemanticSearchHybrid(
	database *sql.DB,
	config searchembedruntime.Config,
	instruments *telemetry.Instruments,
) query.SemanticSearchHybridStore {
	if !config.Enabled {
		return nil
	}
	sqlDB := pgstatus.SQLDB{DB: database}
	metadataDB := newInstrumentedPostgresStore(
		sqlDB,
		"mcp-server",
		semanticSearchVectorMetadataStoreName,
		instruments,
	)
	valueDB := newInstrumentedPostgresStore(
		sqlDB,
		"mcp-server",
		semanticSearchVectorValueStoreName,
		instruments,
	)
	snapshotDB := newInstrumentedPostgresStore(
		sqlDB,
		"mcp-server",
		semanticSearchSnapshotStoreName,
		instruments,
	)
	vectorConfig := query.DefaultPersistedLocalSemanticSearchHybridConfig()
	vectorConfig.ProviderProfileID = config.ProviderProfileID
	vectorConfig.SourceClass = config.SourceClass
	vectorConfig.EmbeddingModelID = config.EmbeddingModelID
	vectorConfig.VectorIndexVersion = config.VectorIndexVersion
	vectorConfig.VectorRetrieval = config.VectorRetrieval
	return query.NewCachedPersistedLocalSemanticSearchHybrid(
		query.NewPostgresSemanticSearchIndexStore(database),
		instrumentedSemanticSearchVectorMetadataStore{
			store:    pgstatus.NewEshuSearchVectorMetadataStore(metadataDB),
			database: metadataDB,
		},
		instrumentedSemanticSearchVectorValueStore{
			store:    pgstatus.NewEshuSearchVectorValueStore(valueDB),
			database: valueDB,
		},
		instrumentedSemanticSearchSnapshotStore{
			store:    query.NewPostgresSemanticSearchSnapshotStore(snapshotDB),
			database: snapshotDB,
		},
		config.Embedder,
		vectorConfig,
	)
}
