// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// liveFixture is one isolated, fully migrated schema with one scope: an
// active generation, a superseded generation and a pending generation, each
// with its own search index rows. Generation ids carry a per-test suffix
// because advisory locks are database-wide, not schema-wide.
type liveFixture struct {
	ctx      context.Context
	db       *sql.DB
	scope    string
	active   string
	old      string
	pending  string
	database postgres.SQLDB
}

func openLiveFixture(t *testing.T) *liveFixture {
	t.Helper()
	// The live-postgres-readiness runner provides the retention family's
	// disposable DSN; a developer run may use ESHU_POSTGRES_DSN instead.
	dsn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")
	if dsn != "" && os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
		t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
	}
	if dsn == "" {
		dsn = os.Getenv("ESHU_POSTGRES_DSN")
	}
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_RETENTION_PROOF_DSN or ESHU_POSTGRES_DSN to run the search index pre-delete live proofs")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "idxret7427", func(ctx context.Context, db *sql.DB) error {
		return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db})
	})
	database.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	f := &liveFixture{
		ctx: ctx, db: database, database: postgres.SQLDB{DB: database},
		scope:   "scope-7427-" + suffix,
		active:  "gen-active-" + suffix,
		old:     "gen-old-" + suffix,
		pending: "gen-pending-" + suffix,
	}
	f.exec(t, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', '{}'::jsonb)`, f.scope)
	f.exec(t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ($1, $4, 'snapshot', now(), now(), 'active', NULL),
       ($2, $4, 'snapshot', now() - interval '10 days', now() - interval '10 days', 'superseded', now() - interval '9 days'),
       ($3, $4, 'snapshot', now(), now(), 'pending', NULL)`, f.active, f.old, f.pending, f.scope)
	f.exec(t, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, f.active, f.scope)
	return f
}

func (f *liveFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.ExecContext(f.ctx, query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// seedIndex gives generation docs documents and docs*termsPerDoc terms. The
// document ids repeat across generations, so the active generation re-mints
// the superseded generation's ids on the same hash leaf (#7427 c7).
func (f *liveFixture) seedIndex(t *testing.T, generation string, docs, termsPerDoc int) {
	t.Helper()
	f.exec(t, `INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
    source_kind, document, document_length, updated_at)
SELECT $1, $2, 'doc-' || d, 'fact-' || d, $1, 'file', '{"t":"x"}'::jsonb, 10, now()
FROM generate_series(0, $3::int - 1) AS d`, f.scope, generation, docs)
	f.exec(t, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
SELECT $1, $2, 'doc-' || d, 'tk-' || k, 'term-' || k, 1
FROM generate_series(0, $3::int - 1) AS d, generate_series(0, $4::int - 1) AS k`, f.scope, generation, docs, termsPerDoc)
}

// counts returns the generation's terms and documents rows.
func (f *liveFixture) counts(t *testing.T, generation string) (terms, documents int64) {
	t.Helper()
	if err := f.db.QueryRowContext(f.ctx, `SELECT
    (SELECT count(*) FROM eshu_search_index_terms WHERE scope_id = $1 AND generation_id = $2),
    (SELECT count(*) FROM eshu_search_index_documents WHERE scope_id = $1 AND generation_id = $2)`,
		f.scope, generation).Scan(&terms, &documents); err != nil {
		t.Fatalf("count search index rows: %v", err)
	}
	return terms, documents
}

func (f *liveFixture) generation(id string) indexretention.Generation {
	return indexretention.Generation{ScopeID: f.scope, GenerationID: id}
}

func unbounded() *indexretention.Budget { return &indexretention.Budget{RowCap: 1 << 40} }
