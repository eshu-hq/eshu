package main

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	semanticSearchVectorMetadataStoreName = "semantic_search_vector_metadata"
	semanticSearchVectorValueStoreName    = "semantic_search_vector_values"
)

type instrumentedSemanticSearchVectorMetadataStore struct {
	store pgstatus.EshuSearchVectorMetadataStore
	db    *pgstatus.InstrumentedDB
}

func (s instrumentedSemanticSearchVectorMetadataStore) ListActive(
	ctx context.Context,
	filter pgstatus.EshuSearchVectorMetadataFilter,
) ([]pgstatus.EshuSearchVectorMetadata, error) {
	return s.store.ListActive(ctx, filter)
}

type instrumentedSemanticSearchVectorValueStore struct {
	store pgstatus.EshuSearchVectorValueStore
	db    *pgstatus.InstrumentedDB
}

func (s instrumentedSemanticSearchVectorValueStore) ListActive(
	ctx context.Context,
	filter pgstatus.EshuSearchVectorValueFilter,
) ([]pgstatus.EshuSearchVectorValue, error) {
	return s.store.ListActive(ctx, filter)
}

func newInstrumentedPostgresStore(
	inner pgstatus.ExecQueryer,
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
func newCodeHybridRanker(config searchembedruntime.Config) query.CodeResultReranker {
	if !config.Enabled {
		return nil
	}
	return query.NewCodeHybridRanker(true)
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
	db *sql.DB,
	config searchembedruntime.Config,
	instruments *telemetry.Instruments,
) query.SemanticSearchHybridStore {
	if !config.Enabled {
		return nil
	}
	sqlDB := pgstatus.SQLDB{DB: db}
	metadataDB := newInstrumentedPostgresStore(
		sqlDB,
		"eshu-api",
		semanticSearchVectorMetadataStoreName,
		instruments,
	)
	valueDB := newInstrumentedPostgresStore(
		sqlDB,
		"eshu-api",
		semanticSearchVectorValueStoreName,
		instruments,
	)
	vectorConfig := query.DefaultPersistedLocalSemanticSearchHybridConfig()
	vectorConfig.ProviderProfileID = config.ProviderProfileID
	vectorConfig.SourceClass = config.SourceClass
	vectorConfig.EmbeddingModelID = config.EmbeddingModelID
	vectorConfig.VectorIndexVersion = config.VectorIndexVersion
	vectorConfig.VectorRetrieval = config.VectorRetrieval
	return query.NewPersistedLocalSemanticSearchHybrid(
		query.NewPostgresSemanticSearchIndexStore(db),
		instrumentedSemanticSearchVectorMetadataStore{
			store: pgstatus.NewEshuSearchVectorMetadataStore(metadataDB),
			db:    metadataDB,
		},
		instrumentedSemanticSearchVectorValueStore{
			store: pgstatus.NewEshuSearchVectorValueStore(valueDB),
			db:    valueDB,
		},
		config.Embedder,
		vectorConfig,
	)
}
