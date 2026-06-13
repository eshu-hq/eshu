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

	loader := NewEshuSearchDocumentSourceLoader(db)
	input, err := loader.LoadSearchDocumentSources(ctx, scopeID, generationID)
	if err != nil {
		t.Fatalf("load sources: %v", err)
	}
	t.Logf("loaded entities=%d files=%d", len(input.ContentEntities), len(input.ContentFiles))
	if len(input.ContentEntities)+len(input.ContentFiles) == 0 {
		t.Fatal("loader returned no content for a scope selected to have content")
	}

	projection := reducer.ProjectSearchDocuments(input)
	t.Logf("curated documents=%d (considered=%d)", projection.Summary.Included, projection.Summary.Considered)
	if projection.Summary.Included == 0 {
		t.Fatal("projection produced no documents")
	}

	writer := reducer.PostgresEshuSearchDocumentWriter{DB: db}
	writeResult, err := writer.WriteEshuSearchDocuments(ctx, reducer.EshuSearchDocumentWrite{
		IntentID:     "searchdoc-proof",
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "github",
		Documents:    projection.Documents,
	})
	if err != nil {
		t.Fatalf("write documents: %v", err)
	}
	t.Logf("wrote=%d retired=%d", writeResult.CanonicalWrites, writeResult.Retired)
	if writeResult.CanonicalWrites != projection.Summary.Included {
		t.Fatalf("wrote %d, want %d", writeResult.CanonicalWrites, projection.Summary.Included)
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
