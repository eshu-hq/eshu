package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEshuSearchVectorMetadataSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := EshuSearchVectorMetadataSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS eshu_search_vector_metadata",
		"scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE",
		"generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE",
		"document_id TEXT NOT NULL",
		"provider_profile_id TEXT NOT NULL",
		"source_class TEXT NOT NULL",
		"embedding_model_id TEXT NOT NULL",
		"embedding_dimensions INTEGER NOT NULL",
		"embedding_content_hash TEXT NOT NULL",
		"vector_index_version TEXT NOT NULL",
		"build_state TEXT NOT NULL",
		"failure_class TEXT NULL",
		"last_success_at TIMESTAMPTZ NULL",
		"PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version)",
		"CHECK (build_state IN ('disabled', 'queued', 'building', 'ready', 'failed', 'stale'))",
		"eshu_search_vector_metadata_state_idx",
		"eshu_search_vector_metadata_model_idx",
		"eshu_search_vector_metadata_model_v2_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("schema missing %q:\n%s", want, sql)
		}
	}
}

func TestBootstrapDefinitionsIncludeEshuSearchVectorMetadata(t *testing.T) {
	t.Parallel()

	var found Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_vector_metadata" {
			found = def
			break
		}
	}
	if found.Name == "" {
		t.Fatal("BootstrapDefinitions missing eshu_search_vector_metadata")
	}
	if found.Path != "schema/data-plane/postgres/003c_eshu_search_vector_metadata.sql" {
		t.Fatalf("Path = %q", found.Path)
	}
	if !strings.Contains(found.SQL, "CREATE TABLE IF NOT EXISTS eshu_search_vector_metadata") {
		t.Fatalf("definition SQL missing vector metadata table:\n%s", found.SQL)
	}
}

func TestEshuSearchVectorMetadataStoreUpsertsBuildState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 21, 0, 0, 0, time.UTC)
	successAt := now.Add(-time.Minute)
	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorMetadataStore(db)

	err := store.Upsert(context.Background(), EshuSearchVectorMetadata{
		ScopeID:              "repo-1",
		GenerationID:         "gen-active",
		DocumentID:           "searchdoc:code:e-1",
		ProviderProfileID:    "local",
		SourceClass:          "search_documents",
		EmbeddingModelID:     "local-hash-v1",
		EmbeddingDimensions:  384,
		EmbeddingContentHash: "sha256:doc",
		VectorIndexVersion:   "vector-v1",
		BuildState:           EshuSearchVectorBuildStateReady,
		FailureClass:         "",
		CreatedAt:            now,
		UpdatedAt:            now,
		LastSuccessAt:        &successAt,
	})
	if err != nil {
		t.Fatalf("Upsert error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(db.execs))
	}
	exec := db.execs[0]
	for _, want := range []string{
		"INSERT INTO eshu_search_vector_metadata",
		"ON CONFLICT (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version) DO UPDATE",
		"build_state = EXCLUDED.build_state",
		"failure_class = EXCLUDED.failure_class",
		"last_success_at = EXCLUDED.last_success_at",
	} {
		if !strings.Contains(exec.query, want) {
			t.Fatalf("upsert query missing %q:\n%s", want, exec.query)
		}
	}
	if got, want := len(exec.args), 14; got != want {
		t.Fatalf("upsert arg count = %d, want %d", got, want)
	}
}

func TestEshuSearchVectorMetadataStoreRejectsInvalidRowsBeforeWrite(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorMetadataStore(db)

	err := store.Upsert(context.Background(), EshuSearchVectorMetadata{
		ScopeID:            "repo-1",
		GenerationID:       "gen-active",
		DocumentID:         "searchdoc:code:e-1",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		BuildState:         EshuSearchVectorBuildState("unknown"),
	})
	if err == nil {
		t.Fatal("Upsert error = nil, want validation error")
	}
	for _, want := range []string{
		"positive embedding dimensions",
		"embedding content hash",
		"invalid eshu search vector build state",
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

func TestEshuSearchVectorMetadataStoreListsOnlyActiveGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 21, 5, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"repo-1",
				"gen-active",
				"searchdoc:code:e-1",
				"semantic-search-default",
				"search_documents",
				"local-hash-v1",
				int64(384),
				"sha256:active",
				"vector-v1",
				string(EshuSearchVectorBuildStateReady),
				"",
				now,
				now,
				sql.NullTime{Time: now, Valid: true},
			}}},
		},
	}
	store := NewEshuSearchVectorMetadataStore(db)

	rows, err := store.ListActive(context.Background(), EshuSearchVectorMetadataFilter{
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
	if got, want := rows[0].GenerationID, "gen-active"; got != want {
		t.Fatalf("generation = %q, want %q", got, want)
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, want := range []string{
		"JOIN ingestion_scopes scope",
		"scope.active_generation_id = meta.generation_id",
		"meta.scope_id = $1",
		"meta.provider_profile_id = $2",
		"meta.source_class = $3",
		"meta.embedding_model_id = $4",
		"meta.vector_index_version = $5",
		"ORDER BY meta.document_id ASC",
		"LIMIT $6",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("active list query missing %q:\n%s", want, q)
		}
	}
}

func TestEshuSearchVectorMetadataStoreClampsActiveListLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewEshuSearchVectorMetadataStore(db)

	rows, err := store.ListActive(context.Background(), EshuSearchVectorMetadataFilter{
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

func TestEshuSearchVectorMetadataStoreFiltersActiveListByDocumentIDs(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewEshuSearchVectorMetadataStore(db)

	_, err := store.ListActive(context.Background(), EshuSearchVectorMetadataFilter{
		ScopeID:            "repo-1",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		DocumentIDs:        []string{"", "searchdoc:code:e-2", "searchdoc:code:e-1", "searchdoc:code:e-2"},
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
		"meta.document_id = ANY($6)",
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

func TestEshuSearchVectorMetadataStoreReadsBoundedStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 21, 10, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"gen-active", string(EshuSearchVectorBuildStateReady), int64(8), now, sql.NullTime{Time: now, Valid: true}},
				{"gen-active", string(EshuSearchVectorBuildStateFailed), int64(2), now, sql.NullTime{}},
			}},
		},
	}
	store := NewEshuSearchVectorMetadataStore(db)

	status, err := store.Status(context.Background(), EshuSearchVectorStatusRequest{
		ScopeID:            "repo-1",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
	})
	if err != nil {
		t.Fatalf("Status error = %v", err)
	}
	if got, want := status.ActiveGenerationID, "gen-active"; got != want {
		t.Fatalf("active generation = %q, want %q", got, want)
	}
	if got, want := status.StateCounts[EshuSearchVectorBuildStateReady], 8; got != want {
		t.Fatalf("ready count = %d, want %d", got, want)
	}
	if got, want := status.StateCounts[EshuSearchVectorBuildStateFailed], 2; got != want {
		t.Fatalf("failed count = %d, want %d", got, want)
	}
	if got, want := status.VectorCount, 10; got != want {
		t.Fatalf("vector count = %d, want %d", got, want)
	}
	q := db.queries[0].query
	for _, want := range []string{
		"JOIN ingestion_scopes scope",
		"scope.active_generation_id = meta.generation_id",
		"meta.provider_profile_id = $2",
		"meta.source_class = $3",
		"GROUP BY scope.active_generation_id, meta.build_state",
		"COUNT(*)",
		"MAX(meta.updated_at)",
		"MAX(meta.last_success_at)",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("status query missing %q:\n%s", want, q)
		}
	}
}
