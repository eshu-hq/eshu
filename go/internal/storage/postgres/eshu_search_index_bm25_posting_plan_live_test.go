// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	indexstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index"

	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// TestEshuSearchIndexBM25PostingLookupUsesCoveringIndexLive proves the
// persisted BM25 scoring read serves its per-posting document lateral from
// the covering posting index without a heap fetch (#7727).
//
// Environment gates (both required to run, shared with the partition proof):
//
//	ESHU_SEARCH_INDEX_PARTITION_LIVE=1
//	ESHU_POSTGRES_DSN=postgresql://eshu:<pw>@localhost:15432/eshu?sslmode=disable
//
// HERMETIC DESIGN — the test never touches the shared
// eshu_search_index_documents table or its indexes. It creates real-named
// throwaway tables in an isolated schema (resolved through search_path, the
// same trick as the partition proof), seeds a production-representative
// shape, and runs EXPLAIN ANALYZE on the production query built by
// indexstore.BuildEshuSearchIndexQuery.
func TestEshuSearchIndexBM25PostingLookupUsesCoveringIndexLive(t *testing.T) {
	database, ctx := openSearchIndexPartitionProofDB(t)
	conn, _ := searchIndexPartitionProofConn(t, ctx, database)
	defer func() { _ = conn.Close() }()

	createBM25PostingProofSchema(t, ctx, conn)
	seedBM25PostingProofRows(t, ctx, conn)

	search := indexstore.EshuSearchIndexSearch{
		ScopeID: "scope-bm25-posting",
		RepoID:  "repo-bm25-posting",
		Query:   "alpha",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-bm25-posting"},
		Limit:   31,
	}
	terms, termKeys := indexstore.SortedSearchIndexTerms(searchhybrid.QueryTerms(search.Query))
	query, args := indexstore.BuildEshuSearchIndexQuery(search, terms, termKeys)
	plan := explainBM25PostingProof(t, ctx, conn, query, args...)
	assertBM25PostingProofIndexOnly(t, plan)

	result := searchBM25PartitionProof(t, ctx, conn, search)
	if len(result.Candidates) == 0 {
		t.Fatal("BM25 posting proof returned no candidates for the hot term")
	}
	for i, candidate := range result.Candidates {
		if candidate.Score <= 0 {
			t.Fatalf("candidate[%d] score = %v, want > 0", i, candidate.Score)
		}
	}

	serviceSearch := search
	serviceSearch.Anchor = searchretrieval.Anchor{Kind: searchretrieval.ScopeKindService, ID: "svc-payments"}
	serviceResult := searchBM25PartitionProof(t, ctx, conn, serviceSearch)
	if len(serviceResult.Candidates) == 0 {
		t.Fatal("BM25 posting proof returned no candidates for the service anchor")
	}
}

func createBM25PostingProofSchema(t *testing.T, ctx context.Context, conn *sql.Conn) {
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
CREATE TABLE eshu_search_index_terms (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL,
    PRIMARY KEY (scope_id, generation_id, term_key, document_id)
);
CREATE TABLE eshu_search_index_stats (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    document_count INTEGER NOT NULL,
    average_document_length DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
CREATE INDEX eshu_search_index_documents_bm25_posting_idx
    ON eshu_search_index_documents (scope_id, generation_id, document_id) INCLUDE (repo_id, document_length);
`); err != nil {
		t.Fatalf("create BM25 posting proof schema: %v", err)
	}
}

func seedBM25PostingProofRows(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()
	const (
		scopeID      = "scope-bm25-posting"
		generationID = "gen-bm25-posting"
		repoID       = "repo-bm25-posting"
		docCount     = 2000
		hotPostings  = 800
	)
	if _, err := conn.ExecContext(ctx, `
INSERT INTO ingestion_scopes(scope_id, active_generation_id)
VALUES ('scope-bm25-posting', 'gen-bm25-posting');
INSERT INTO eshu_search_index_stats(scope_id, generation_id, document_count, average_document_length)
VALUES ('scope-bm25-posting', 'gen-bm25-posting', 2000, 100.0);
`); err != nil {
		t.Fatalf("seed BM25 posting scopes/stats: %v", err)
	}
	for doc := 0; doc < docCount; doc++ {
		id := fmt.Sprintf("doc-%04d", doc)
		payload, err := json.Marshal(searchIndexDocumentFixture(id, repoID, "Alpha runbook"))
		if err != nil {
			t.Fatalf("marshal BM25 posting proof doc: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `
INSERT INTO eshu_search_index_documents(scope_id, generation_id, document_id, repo_id, source_kind, document, document_length)
VALUES ('scope-bm25-posting', 'gen-bm25-posting', $1, 'repo-bm25-posting', 'runtime-summary', $2::jsonb, 100)
`, id, string(payload)); err != nil {
			t.Fatalf("insert BM25 posting proof doc %s: %v", id, err)
		}
		if doc < hotPostings {
			insertBM25TermLive(t, ctx, conn, scopeID, generationID, id, "alpha", 1+doc%3)
		}
	}
	if _, err := conn.ExecContext(ctx, "VACUUM (ANALYZE) eshu_search_index_documents"); err != nil {
		t.Fatalf("vacuum BM25 posting proof documents: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM (ANALYZE) eshu_search_index_terms"); err != nil {
		t.Fatalf("vacuum BM25 posting proof terms: %v", err)
	}
}

func explainBM25PostingProof(t *testing.T, ctx context.Context, conn *sql.Conn, query string, args ...any) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var raw []byte
	if err := conn.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain BM25 posting query: %v", err)
	}
	return string(raw)
}

func assertBM25PostingProofIndexOnly(t *testing.T, plan string) {
	t.Helper()
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(plan), &plans); err != nil || len(plans) == 0 {
		t.Fatalf("decode BM25 posting plan: %v", err)
	}
	var indexOnly, postingIndex, heapFetches int
	var seqScannedDocs bool
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		if nodeType == "Index Only Scan" {
			indexOnly++
		}
		if name, _ := node["Index Name"].(string); strings.Contains(name, "bm25_posting_idx") {
			postingIndex++
			if v, ok := node["Heap Fetches"].(float64); ok {
				heapFetches += int(v)
			}
		}
		if nodeType == "Seq Scan" {
			if rel, _ := node["Relation Name"].(string); rel == "eshu_search_index_documents" {
				seqScannedDocs = true
			}
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				if childNode, ok := child.(map[string]any); ok {
					walk(childNode)
				}
			}
		}
	}
	walk(plans[0].Plan)
	t.Logf("BM25 posting plan: index-only=%d posting-index=%d heap-fetches=%d", indexOnly, postingIndex, heapFetches)
	if postingIndex == 0 {
		t.Fatalf("BM25 plan never uses the covering posting index:\n%s", planJSONSummary(t, plan))
	}
	if heapFetches != 0 {
		t.Fatalf("BM25 plan has %d heap fetches, want 0:\n%s", heapFetches, planJSONSummary(t, plan))
	}
	if seqScannedDocs {
		t.Fatalf("BM25 plan sequentially scans the documents table:\n%s", planJSONSummary(t, plan))
	}
}
