// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

func TestEshuSearchIndexBM25PartitionedTermsPrunedAndOrderEquivalentLive(t *testing.T) {
	db, ctx := openSearchIndexPartitionProofDB(t)
	controlConn, _ := searchIndexPartitionProofConn(t, ctx, db)
	defer func() { _ = controlConn.Close() }()
	candidateConn, _ := searchIndexPartitionProofConn(t, ctx, db)
	defer func() { _ = candidateConn.Close() }()

	createBM25PartitionProofSchema(t, ctx, controlConn, false)
	createBM25PartitionProofSchema(t, ctx, candidateConn, true)
	seedBM25PartitionProofRows(t, ctx, controlConn)
	seedBM25PartitionProofRows(t, ctx, candidateConn)

	search := EshuSearchIndexSearch{
		ScopeID: "scope-bm25-active",
		RepoID:  "repo-bm25",
		Query:   "alpha beta",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-bm25"},
		Limit:   10,
	}
	controlResult := searchBM25PartitionProof(t, ctx, controlConn, search)
	candidateResult := searchBM25PartitionProof(t, ctx, candidateConn, search)
	assertBM25PartitionProofEquivalent(t, controlResult, candidateResult)

	terms, termKeys := sortedSearchIndexTerms(searchhybrid.QueryTerms(search.Query))
	query, args := buildEshuSearchIndexQuery(search, terms, termKeys)
	var raw []byte
	if err := candidateConn.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain partitioned BM25 query: %v", err)
	}
	childScans := scannedChildPartitionsLive(string(raw), "eshu_search_index_terms")
	if len(childScans) == 0 {
		t.Fatalf("BM25 plan scanned no child partitions:\n%s", planJSONSummary(t, string(raw)))
	}
	if len(childScans) >= 64 {
		t.Fatalf("BM25 plan scanned all partitions; want scope pruning:\n%s", planJSONSummary(t, string(raw)))
	}
}

func createBM25PartitionProofSchema(
	t *testing.T,
	ctx context.Context,
	conn *sql.Conn,
	partitionedTerms bool,
) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    active_generation_id TEXT
);
CREATE TABLE eshu_search_index_documents (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    document JSONB NOT NULL,
    document_length INTEGER NOT NULL,
    PRIMARY KEY (scope_id, generation_id, document_id)
);
CREATE TABLE eshu_search_index_stats (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    document_count INTEGER NOT NULL,
    average_document_length DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
`); err != nil {
		t.Fatalf("create BM25 proof base schema: %v", err)
	}
	if partitionedTerms {
		if _, err := conn.ExecContext(ctx, `
CREATE TABLE eshu_search_index_terms (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL,
    PRIMARY KEY (scope_id, generation_id, term_key, document_id)
) PARTITION BY HASH (scope_id)
`); err != nil {
			t.Fatalf("create partitioned BM25 terms: %v", err)
		}
		for remainder := 0; remainder < 64; remainder++ {
			if _, err := conn.ExecContext(ctx, fmt.Sprintf(
				"CREATE TABLE eshu_search_index_terms_p%02d PARTITION OF eshu_search_index_terms FOR VALUES WITH (MODULUS 64, REMAINDER %d)",
				remainder,
				remainder,
			)); err != nil {
				t.Fatalf("create BM25 terms partition %d: %v", remainder, err)
			}
		}
		return
	}
	if _, err := conn.ExecContext(ctx, `
CREATE TABLE eshu_search_index_terms (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL,
    PRIMARY KEY (scope_id, generation_id, term_key, document_id)
)
`); err != nil {
		t.Fatalf("create regular BM25 terms: %v", err)
	}
}

func seedBM25PartitionProofRows(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()
	docs := []struct {
		id     string
		title  string
		length int
	}{
		{"doc-alpha-beta", "Alpha beta runbook", 100},
		{"doc-alpha", "Alpha incident notes", 80},
		{"doc-beta", "Beta escalation guide", 120},
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO ingestion_scopes(scope_id, active_generation_id)
VALUES
    ('scope-bm25-active', 'gen-active'),
    ('scope-bm25-background', 'gen-background');
INSERT INTO eshu_search_index_stats(scope_id, generation_id, document_count, average_document_length)
VALUES
    ('scope-bm25-active', 'gen-active', 3, 100.0),
    ('scope-bm25-active', 'gen-stale', 1, 10.0),
    ('scope-bm25-background', 'gen-background', 1, 10.0);
`); err != nil {
		t.Fatalf("seed BM25 scopes/stats: %v", err)
	}
	for _, doc := range docs {
		payload, err := json.Marshal(searchIndexDocumentFixture(doc.id, "repo-bm25", doc.title))
		if err != nil {
			t.Fatalf("marshal BM25 proof doc: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `
INSERT INTO eshu_search_index_documents(scope_id, generation_id, document_id, repo_id, source_kind, document, document_length)
VALUES ($1, 'gen-active', $2, 'repo-bm25', $3, $4::jsonb, $5)
`, "scope-bm25-active", doc.id, string(searchdocs.SourceKindRuntimeSummary), string(payload), doc.length); err != nil {
			t.Fatalf("insert BM25 proof doc %s: %v", doc.id, err)
		}
	}
	insertBM25TermLive(t, ctx, conn, "scope-bm25-active", "gen-active", "doc-alpha-beta", "alpha", 3)
	insertBM25TermLive(t, ctx, conn, "scope-bm25-active", "gen-active", "doc-alpha-beta", "beta", 1)
	insertBM25TermLive(t, ctx, conn, "scope-bm25-active", "gen-active", "doc-alpha", "alpha", 1)
	insertBM25TermLive(t, ctx, conn, "scope-bm25-active", "gen-active", "doc-beta", "beta", 4)
	insertBM25TermLive(t, ctx, conn, "scope-bm25-active", "gen-stale", "doc-stale", "alpha", 99)
	insertBM25TermLive(t, ctx, conn, "scope-bm25-background", "gen-background", "doc-bg", "alpha", 99)
	if _, err := conn.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatalf("analyze BM25 proof schema: %v", err)
	}
}

func insertBM25TermLive(
	t *testing.T,
	ctx context.Context,
	conn *sql.Conn,
	scopeID string,
	generationID string,
	documentID string,
	term string,
	frequency int,
) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, `
INSERT INTO eshu_search_index_terms(scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, $3, $4, $5, $6)
`, scopeID, generationID, documentID, searchhybrid.TermKey(term), term, frequency); err != nil {
		t.Fatalf("insert BM25 term %s/%s/%s/%s: %v", scopeID, generationID, documentID, term, err)
	}
}

func searchBM25PartitionProof(
	t *testing.T,
	ctx context.Context,
	conn *sql.Conn,
	search EshuSearchIndexSearch,
) EshuSearchIndexSearchResult {
	t.Helper()
	result, err := NewEshuSearchIndexStore(searchIndexSQLConn{conn: conn}).Search(ctx, search)
	if err != nil {
		t.Fatalf("search BM25 proof: %v", err)
	}
	return result
}

type searchIndexSQLConn struct {
	conn *sql.Conn
}

func (c searchIndexSQLConn) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c searchIndexSQLConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func assertBM25PartitionProofEquivalent(
	t *testing.T,
	control EshuSearchIndexSearchResult,
	candidate EshuSearchIndexSearchResult,
) {
	t.Helper()
	if control.IndexedDocumentCount != candidate.IndexedDocumentCount {
		t.Fatalf("document count control=%d candidate=%d", control.IndexedDocumentCount, candidate.IndexedDocumentCount)
	}
	if len(control.Candidates) != len(candidate.Candidates) {
		t.Fatalf("candidate count control=%d candidate=%d", len(control.Candidates), len(candidate.Candidates))
	}
	for i := range control.Candidates {
		controlCandidate := control.Candidates[i]
		candidateCandidate := candidate.Candidates[i]
		if controlCandidate.Document.ID != candidateCandidate.Document.ID {
			t.Fatalf("candidate[%d] id control=%s candidate=%s", i, controlCandidate.Document.ID, candidateCandidate.Document.ID)
		}
		if math.Abs(controlCandidate.Score-candidateCandidate.Score) > 1e-12 {
			t.Fatalf("candidate[%d] score control=%.15f candidate=%.15f",
				i, controlCandidate.Score, candidateCandidate.Score)
		}
	}
}
