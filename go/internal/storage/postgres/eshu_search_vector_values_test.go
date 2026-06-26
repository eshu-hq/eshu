// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestEshuSearchVectorValuesSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := EshuSearchVectorValuesSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS eshu_search_vector_values",
		"scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE",
		"generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE",
		"document_id TEXT NOT NULL",
		"provider_profile_id TEXT NOT NULL",
		"source_class TEXT NOT NULL",
		"embedding_model_id TEXT NOT NULL",
		"embedding_dimensions INTEGER NOT NULL",
		"embedding_content_hash TEXT NOT NULL",
		"vector_index_version TEXT NOT NULL",
		"vector_values DOUBLE PRECISION[] NOT NULL",
		"PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version)",
		"CHECK (cardinality(vector_values) = embedding_dimensions)",
		"eshu_search_vector_values_model_idx",
		"eshu_search_vector_values_model_v2_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("schema missing %q:\n%s", want, sql)
		}
	}
}

func TestBootstrapDefinitionsIncludeEshuSearchVectorValues(t *testing.T) {
	t.Parallel()

	var found Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_vector_values" {
			found = def
			break
		}
	}
	if found.Name == "" {
		t.Fatal("BootstrapDefinitions missing eshu_search_vector_values")
	}
	if found.Path != "go/internal/storage/postgres/migrations/003d_eshu_search_vector_values.sql" {
		t.Fatalf("Path = %q", found.Path)
	}
	if !strings.Contains(found.SQL, "CREATE TABLE IF NOT EXISTS eshu_search_vector_values") {
		t.Fatalf("definition SQL missing vector values table:\n%s", found.SQL)
	}
}

func TestEshuSearchVectorValueStoreUpsertsBoundedVector(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 22, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorValueStore(db)

	err := store.Upsert(context.Background(), EshuSearchVectorValue{
		ScopeID:              "repo-1",
		GenerationID:         "gen-active",
		DocumentID:           "searchdoc:code:e-1",
		ProviderProfileID:    "local",
		SourceClass:          "search_documents",
		EmbeddingModelID:     "local-hash-v1",
		EmbeddingDimensions:  4,
		EmbeddingContentHash: "sha256:doc",
		VectorIndexVersion:   "vector-v1",
		VectorValues:         []float64{0.25, -0.5, 0, 1},
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		t.Fatalf("Upsert error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(db.execs))
	}
	exec := db.execs[0]
	for _, want := range []string{
		"INSERT INTO eshu_search_vector_values",
		"ON CONFLICT (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version) DO UPDATE",
		"embedding_dimensions = EXCLUDED.embedding_dimensions",
		"embedding_content_hash = EXCLUDED.embedding_content_hash",
		"vector_values = EXCLUDED.vector_values",
		"updated_at = EXCLUDED.updated_at",
	} {
		if !strings.Contains(exec.query, want) {
			t.Fatalf("upsert query missing %q:\n%s", want, exec.query)
		}
	}
	if got, want := len(exec.args), 12; got != want {
		t.Fatalf("upsert arg count = %d, want %d", got, want)
	}
}

func TestEshuSearchVectorValueStoreRejectsInvalidRowsBeforeWrite(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorValueStore(db)

	err := store.Upsert(context.Background(), EshuSearchVectorValue{
		ScopeID:              "repo-1",
		GenerationID:         "gen-active",
		DocumentID:           "searchdoc:code:e-1",
		ProviderProfileID:    "local",
		SourceClass:          "search_documents",
		EmbeddingModelID:     "local-hash-v1",
		EmbeddingDimensions:  2,
		EmbeddingContentHash: "sha256:doc",
		VectorIndexVersion:   "vector-v1",
		VectorValues:         []float64{0.1, math.Inf(1)},
	})
	if err == nil {
		t.Fatal("Upsert error = nil, want validation error")
	}
	for _, want := range []string{
		"finite vector value",
		"created_at",
		"updated_at",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error missing %q: %v", want, err)
		}
	}
	if len(db.execs) != 0 {
		t.Fatalf("execs = %d, want 0", len(db.execs))
	}
}

func TestEshuSearchVectorValueStoreRejectsDimensionMismatchBeforeWrite(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorValueStore(db)

	err := store.Upsert(context.Background(), EshuSearchVectorValue{
		ScopeID:              "repo-1",
		GenerationID:         "gen-active",
		DocumentID:           "searchdoc:code:e-1",
		ProviderProfileID:    "local",
		SourceClass:          "search_documents",
		EmbeddingModelID:     "local-hash-v1",
		EmbeddingDimensions:  3,
		EmbeddingContentHash: "sha256:doc",
		VectorIndexVersion:   "vector-v1",
		VectorValues:         []float64{0.1, 0.2},
		CreatedAt:            time.Date(2026, 6, 14, 22, 35, 0, 0, time.UTC),
		UpdatedAt:            time.Date(2026, 6, 14, 22, 35, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("Upsert error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "vector length must match embedding dimensions") {
		t.Fatalf("validation error = %v", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("execs = %d, want 0", len(db.execs))
	}
}

func TestEshuSearchVectorValueStoreListsOnlyActiveGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 22, 40, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"repo-1",
				"gen-active",
				"searchdoc:code:e-1",
				"semantic-search-default",
				"search_documents",
				"local-hash-v1",
				int64(3),
				"sha256:active",
				"vector-v1",
				[]float64{0.2, 0.4, 0.6},
				now,
				now,
			}}},
		},
	}
	store := NewEshuSearchVectorValueStore(db)

	rows, err := store.ListActive(context.Background(), EshuSearchVectorValueFilter{
		ScopeID:            "repo-1",
		ProviderProfileID:  "semantic-search-default",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ListActive error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, want := row.GenerationID, "gen-active"; got != want {
		t.Fatalf("generation = %q, want %q", got, want)
	}
	if got, want := len(row.VectorValues), 3; got != want {
		t.Fatalf("vector length = %d, want %d", got, want)
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, want := range []string{
		"JOIN ingestion_scopes scope",
		"scope.active_generation_id = vec.generation_id",
		"JOIN eshu_search_vector_metadata meta",
		"meta.document_id = vec.document_id",
		"meta.provider_profile_id = vec.provider_profile_id",
		"meta.source_class = vec.source_class",
		"meta.embedding_content_hash = vec.embedding_content_hash",
		"meta.build_state = 'ready'",
		"vec.scope_id = $1",
		"vec.provider_profile_id = $2",
		"vec.source_class = $3",
		"vec.embedding_model_id = $4",
		"vec.vector_index_version = $5",
		"ORDER BY vec.document_id ASC",
		"LIMIT $6",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("active list query missing %q:\n%s", want, q)
		}
	}
}

func TestEshuSearchVectorValueStoreClampsActiveListLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewEshuSearchVectorValueStore(db)

	rows, err := store.ListActive(context.Background(), EshuSearchVectorValueFilter{
		ScopeID:            "repo-1",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              1000,
	})
	if err != nil {
		t.Fatalf("ListActive error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	if got, want := db.queries[0].args[5], 500; got != want {
		t.Fatalf("limit arg = %v, want %d", got, want)
	}
}

func TestEshuSearchVectorValueStoreFiltersActiveListByDocumentIDs(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewEshuSearchVectorValueStore(db)

	_, err := store.ListActive(context.Background(), EshuSearchVectorValueFilter{
		ScopeID:            "repo-1",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		DocumentIDs:        []string{"searchdoc:code:e-2", "", "searchdoc:code:e-1", "searchdoc:code:e-2"},
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ListActive error = %v", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, want := range []string{
		"vec.document_id = ANY($6)",
		"LIMIT $7",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("active list query missing %q:\n%s", want, q)
		}
	}
	gotDocumentIDs := fmt.Sprint(db.queries[0].args[5])
	for _, want := range []string{"searchdoc:code:e-1", "searchdoc:code:e-2"} {
		if !strings.Contains(gotDocumentIDs, want) {
			t.Fatalf("document ids arg = %v, want %s", gotDocumentIDs, want)
		}
	}
	if got, want := db.queries[0].args[6], 10; got != want {
		t.Fatalf("limit arg = %v, want %d", got, want)
	}
}
