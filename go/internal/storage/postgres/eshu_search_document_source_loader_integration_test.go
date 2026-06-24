// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestEshuSearchDocumentProjectionRoundTripLive proves the curated search
// read-model data path end to end against a live Postgres corpus: it loads a
// repository scope's indexed content through the source loader, curates it with
// the reducer projection, writes the derived facts with the reducer writer, and
// reads them back through the active-generation store. It is skipped unless
// ESHU_SEARCHDOC_PROOF_DSN is set so CI without a database is unaffected. The
// test writes facts for the scope's active generation and deletes them on
// cleanup so the corpus is left as found.
func TestEshuSearchDocumentProjectionRoundTripLive(t *testing.T) {
	dsn := os.Getenv("ESHU_SEARCHDOC_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_SEARCHDOC_PROOF_DSN to run the live search-document round-trip proof")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	db := SQLDB{DB: sqlDB}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pick a repository scope with content and its active generation.
	var scopeID, generationID string
	err = sqlDB.QueryRowContext(ctx, `
		SELECT s.scope_id, s.active_generation_id
		FROM ingestion_scopes s
		WHERE s.scope_kind = 'repository' AND s.active_generation_id IS NOT NULL
		  AND EXISTS (SELECT 1 FROM content_entities ce WHERE ce.repo_id = s.payload->>'repo_id')
		LIMIT 1`).Scan(&scopeID, &generationID)
	if err != nil {
		t.Fatalf("select repository scope with content: %v", err)
	}
	t.Logf("proof scope=%s generation=%s", scopeID, generationID)

	t.Cleanup(func() {
		_, _ = sqlDB.ExecContext(context.Background(),
			`DELETE FROM fact_records WHERE fact_kind = $1 AND scope_id = $2 AND generation_id = $3`,
			EshuSearchDocumentFactKind, scopeID, generationID)
	})

	// Stream the scope's content through the bounded paginated loader, projecting
	// and writing each page incrementally, then finalize the authoritative retire
	// once — the production #3440 streaming path.
	loader := NewEshuSearchDocumentSourceLoader(db)
	writer := reducer.PostgresEshuSearchDocumentWriter{DB: db}
	session, err := writer.BeginEshuSearchDocumentWrite(ctx, reducer.EshuSearchDocumentWriteBegin{
		IntentID:     "searchdoc-proof",
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "github",
	})
	if err != nil {
		t.Fatalf("begin write: %v", err)
	}

	var consideredTotal, includedTotal, loadedEntities, loadedFiles int
	streamErr := loader.StreamSearchDocumentSources(ctx, scopeID, generationID,
		func(input reducer.SearchDocumentProjectionInput) error {
			loadedEntities += len(input.ContentEntities)
			loadedFiles += len(input.ContentFiles)
			projection := reducer.ProjectSearchDocuments(input)
			consideredTotal += projection.Summary.Considered
			includedTotal += projection.Summary.Included
			return session.InsertPage(ctx, projection.Documents)
		})
	if streamErr != nil {
		t.Fatalf("stream sources: %v", streamErr)
	}
	t.Logf("loaded entities=%d files=%d", loadedEntities, loadedFiles)
	if loadedEntities+loadedFiles == 0 {
		t.Fatal("loader returned no content for a scope selected to have content")
	}
	t.Logf("curated documents=%d (considered=%d)", includedTotal, consideredTotal)
	if includedTotal == 0 {
		t.Fatal("projection produced no documents")
	}

	writeResult, err := session.Finalize(ctx)
	if err != nil {
		t.Fatalf("finalize documents: %v", err)
	}
	t.Logf("wrote=%d retired=%d", writeResult.CanonicalWrites, writeResult.Retired)
	if writeResult.CanonicalWrites != includedTotal {
		t.Fatalf("wrote %d, want %d", writeResult.CanonicalWrites, includedTotal)
	}

	store := NewEshuSearchDocumentStore(db)
	rows, err := store.ListActiveDocuments(ctx, EshuSearchDocumentFilter{ScopeID: scopeID, Limit: 10})
	if err != nil {
		t.Fatalf("list active documents: %v", err)
	}
	t.Logf("read back active documents (page)=%d", len(rows))
	if len(rows) == 0 {
		t.Fatal("read model returned no active documents after write")
	}
	for _, row := range rows {
		if row.GenerationID != generationID {
			t.Fatalf("active row generation = %q, want %q", row.GenerationID, generationID)
		}
		if row.Document.ID == "" || len(row.Document.GraphHandles) == 0 {
			t.Fatalf("active row missing document id or handles: %+v", row.Document)
		}
	}
}
