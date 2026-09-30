// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/eshusearch"
)

const (
	supersedeProofScope = "scope-7458"
	supersedeProofRepo  = "repo-7458"
	supersedeProofGenG  = "gen-7458-g"
	supersedeProofGenH  = "gen-7458-h"
)

// supersedeProofSeed seeds scope S with G active, H a newer pending generation,
// and 600 small content_files for the repository: the production file page
// size (256) yields three pages of 256, 256 and 88.
const supersedeProofSeed = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES ('scope-7458', 'repository', 'git', 'proof/7458', 'git', 'proof/7458',
          now(), now(), 'active', 'gen-7458-g', '{"repo_id":"repo-7458"}'::jsonb);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES ('gen-7458-g', 'scope-7458', 'snapshot', now() - interval '2 hours',
          now() - interval '2 hours', 'active', now() - interval '2 hours');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('gen-7458-h', 'scope-7458', 'snapshot', now() - interval '1 hour',
          now() - interval '1 hour', 'pending');
INSERT INTO content_files (
    repo_id, relative_path, content, content_hash, line_count, language, indexed_at
) SELECT 'repo-7458', 'src/f' || lpad(i::text, 4, '0') || '.go',
         'package f' || i || E'\nfunc F' || i || '() {}', md5(i::text), 2, 'go', now()
  FROM generate_series(1, 600) AS i;
`

// activateSupersedeProofGenerationH commits the projector's activation of H for
// scope S with the projector's own statement shapes (projector_queue_sql.go):
// G is superseded, H becomes active, and the scope pointer moves to H.
func activateSupersedeProofGenerationH(ctx context.Context, sqlDB *sql.DB) error {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded', superseded_at = $1
WHERE scope_id = $2 AND generation_id = $3`, now, supersedeProofScope, supersedeProofGenG); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, activateProjectorGenerationQuery,
		now, supersedeProofScope, supersedeProofGenH); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, updateProjectorScopeGenerationQuery,
		now, supersedeProofScope, supersedeProofGenH); err != nil {
		return err
	}
	return tx.Commit()
}

// activatingLoader wraps the real source loader and, after the first page has
// been delivered and processed, runs onFirstPage once. It models the projector
// activating a newer generation while the older generation's handler streams.
type activatingLoader struct {
	inner       eshusearch.SearchDocumentSourceLoader
	onFirstPage func() error
	pages       int
	pageDocs    []int
}

func (l *activatingLoader) StreamSearchDocumentSources(
	ctx context.Context,
	scopeID, generationID string,
	page func(eshusearch.SearchDocumentProjectionInput) error,
) error {
	return l.inner.StreamSearchDocumentSources(ctx, scopeID, generationID,
		func(input eshusearch.SearchDocumentProjectionInput) error {
			l.pageDocs = append(l.pageDocs, len(eshusearch.ProjectSearchDocuments(input).Documents))
			err := page(input)
			l.pages++
			if err == nil && l.pages == 1 && l.onFirstPage != nil {
				return l.onFirstPage()
			}
			return err
		})
}

// supersedeProofCounts is what one (scope, generation) holds in the search
// tables.
type supersedeProofCounts struct {
	facts, indexDocs, stats int
	state                   string
}

func readSupersedeProofCounts(ctx context.Context, t *testing.T, sqlDB *sql.DB, generationID string) supersedeProofCounts {
	t.Helper()
	var c supersedeProofCounts
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_records WHERE fact_kind = $3 AND scope_id = $1 AND generation_id = $2`,
		supersedeProofScope, generationID, EshuSearchDocumentFactKind).Scan(&c.facts); err != nil {
		t.Fatalf("count facts %s: %v", generationID, err)
	}
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM eshu_search_index_documents WHERE scope_id = $1 AND generation_id = $2`,
		supersedeProofScope, generationID).Scan(&c.indexDocs); err != nil {
		t.Fatalf("count index documents %s: %v", generationID, err)
	}
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM eshu_search_index_stats WHERE scope_id = $1 AND generation_id = $2`,
		supersedeProofScope, generationID).Scan(&c.stats); err != nil {
		t.Fatalf("count index stats %s: %v", generationID, err)
	}
	err := sqlDB.QueryRowContext(ctx, `
SELECT state FROM eshu_search_document_projection_state WHERE scope_id = $1 AND generation_id = $2`,
		supersedeProofScope, generationID).Scan(&c.state)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("read projection state %s: %v", generationID, err)
	}
	return c
}

// TestEshuSearchDocumentHandlerAbandonsSupersededGenerationLive is the #7458
// interleave proof. Generation G streams three file pages; after page 1 the
// projector activates the newer generation H. With the freshness fence G must
// stop after the page in flight, never retire, never write index stats, and be
// acked as superseded; H's later run must then leave the active-generation
// reader with exactly H's documents and nothing pending for the scope.
//
// Run locally against a disposable database:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://user:pass@localhost:<port>/<db> \
//	go test ./internal/storage/postgres \
//	  -run TestEshuSearchDocumentHandlerAbandonsSupersededGenerationLive -count=1 -v
func TestEshuSearchDocumentHandlerAbandonsSupersededGenerationLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	sqlDB, _ := openServiceLineageSchemaLive(ctx, t, "eshu_7458_supersede")
	if err := ApplyBootstrap(ctx, SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, supersedeProofSeed); err != nil {
		t.Fatalf("seed proof fixture: %v", err)
	}
	database := SQLDB{DB: sqlDB}

	newHandler := func(loader eshusearch.SearchDocumentSourceLoader) eshusearch.EshuSearchDocumentHandler {
		return eshusearch.EshuSearchDocumentHandler{
			Loader: loader,
			Writer: eshusearch.PostgresEshuSearchDocumentWriter{
				DB:              database,
				ProjectionState: NewEshuSearchDocumentProjectionStateStore(database),
			},
			GenerationCheck: NewGenerationFreshnessCheck(database),
		}
	}
	intent := func(generationID string) reducercontract.Intent {
		return reducercontract.Intent{
			IntentID:     "intent-" + generationID,
			ScopeID:      supersedeProofScope,
			GenerationID: generationID,
			SourceSystem: "git",
			Domain:       eshusearch.DomainEshuSearchDocument,
		}
	}

	// G streams, H activates after page 1.
	gLoader := &activatingLoader{
		inner:       NewEshuSearchDocumentSourceLoader(database),
		onFirstPage: func() error { return activateSupersedeProofGenerationH(ctx, sqlDB) },
	}
	gResult, err := newHandler(gLoader).Handle(ctx, intent(supersedeProofGenG))
	if err != nil {
		t.Fatalf("G Handle error = %v, want a superseded result", err)
	}
	g := readSupersedeProofCounts(ctx, t, sqlDB, supersedeProofGenG)
	t.Logf("G after supersede: result=%s pages_streamed=%d page_docs=%v facts=%d index_docs=%d stats=%d state=%q evidence=%q",
		gResult.Status, gLoader.pages, gLoader.pageDocs, g.facts, g.indexDocs, g.stats, g.state, gResult.EvidenceSummary)

	if len(gLoader.pageDocs) < 2 {
		t.Fatalf("loader delivered %d pages; the fixture must stream at least two", len(gLoader.pageDocs))
	}
	page1 := gLoader.pageDocs[0]
	if gResult.Status != reducercontract.ResultStatusSuperseded {
		t.Errorf("G status = %q, want %q", gResult.Status, reducercontract.ResultStatusSuperseded)
	}
	if g.facts != page1 || g.indexDocs != page1 {
		t.Errorf("G rows facts=%d index_docs=%d, want exactly page 1 (%d)",
			g.facts, g.indexDocs, page1)
	}
	if gResult.CanonicalWrites != page1 {
		t.Errorf("G CanonicalWrites = %d, want %d", gResult.CanonicalWrites, page1)
	}
	if g.stats != 0 {
		t.Errorf("G eshu_search_index_stats rows = %d, want 0 (Finalize must not run)", g.stats)
	}
	if g.state != "building" {
		t.Errorf("G projection state = %q, want building (not finalized, not cancelled)", g.state)
	}

	// H runs to completion.
	hLoader := &activatingLoader{inner: NewEshuSearchDocumentSourceLoader(database)}
	hResult, err := newHandler(hLoader).Handle(ctx, intent(supersedeProofGenH))
	if err != nil {
		t.Fatalf("H Handle error = %v", err)
	}
	total := 0
	for _, n := range hLoader.pageDocs {
		total += n
	}
	if total <= page1 {
		t.Fatalf("H streamed %d documents, want more than G's page 1 (%d)", total, page1)
	}
	if hResult.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("H status = %q, want succeeded", hResult.Status)
	}
	h := readSupersedeProofCounts(ctx, t, sqlDB, supersedeProofGenH)
	if h.facts != total || h.indexDocs != total || h.stats != 1 || h.state != "ready" {
		t.Errorf("H rows facts=%d index_docs=%d stats=%d state=%q, want %d/%d/1/ready",
			h.facts, h.indexDocs, h.stats, h.state, total, total)
	}

	store := NewEshuSearchDocumentStore(database)
	seen := map[string]bool{}
	for offset := 0; ; offset += 500 {
		rows, err := store.ListActiveDocuments(ctx, EshuSearchDocumentFilter{
			ScopeID: supersedeProofScope, Limit: 500, Offset: offset,
		})
		if err != nil {
			t.Fatalf("ListActiveDocuments: %v", err)
		}
		for _, row := range rows {
			if row.GenerationID != supersedeProofGenH {
				t.Fatalf("active reader returned generation %q, want only %q", row.GenerationID, supersedeProofGenH)
			}
			seen[row.Document.ID] = true
		}
		if len(rows) < 500 {
			break
		}
	}
	if len(seen) != total {
		t.Errorf("active reader returned %d distinct documents, want H's %d", len(seen), total)
	}

	pending, err := NewEshuSearchDocumentPendingStore(database).ListPendingSearchDocumentScopes(ctx, 10)
	if err != nil {
		t.Fatalf("ListPendingSearchDocumentScopes: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending scopes = %+v, want none after H is ready", pending)
	}
}
