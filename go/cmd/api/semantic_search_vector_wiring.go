package main

import (
	"context"
	"database/sql"
	"strings"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
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

func newLocalSemanticSearchHybrid(
	db *sql.DB,
	embedder string,
	instruments *telemetry.Instruments,
) query.SemanticSearchHybridStore {
	if strings.TrimSpace(embedder) == "" {
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
	return query.NewDefaultPersistedLocalSemanticSearchHybrid(
		query.NewPostgresSemanticSearchIndexStore(db),
		instrumentedSemanticSearchVectorMetadataStore{
			store: pgstatus.NewEshuSearchVectorMetadataStore(metadataDB),
			db:    metadataDB,
		},
		instrumentedSemanticSearchVectorValueStore{
			store: pgstatus.NewEshuSearchVectorValueStore(valueDB),
			db:    valueDB,
		},
	)
}
