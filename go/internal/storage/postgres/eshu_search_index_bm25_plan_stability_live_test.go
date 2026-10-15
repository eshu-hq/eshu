// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	indexstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// TestEshuSearchIndexBM25ScoreStableAcrossPlansLive proves the persisted BM25
// score is byte-identical under different planner choices for a
// three-or-more-term query (#7737). Float addition is not associative, so a
// bare SUM over the per-term contributions lets the plan-dependent row order
// change score bits and tie order; the ordered aggregate in
// indexstore.BuildEshuSearchIndexQuery fixes the summation order on the stable
// per-term key.
//
// Environment gates (both required to run, shared with the partition proof):
//
//	ESHU_SEARCH_INDEX_PARTITION_LIVE=1
//	ESHU_POSTGRES_DSN=postgresql://eshu:<pw>@localhost:15432/eshu?sslmode=disable
//
// HERMETIC DESIGN — like the partition and posting proofs, the test creates
// real-named throwaway tables in an isolated schema and runs the production
// query built by indexstore.BuildEshuSearchIndexQuery; it never touches the
// shared index tables.
func TestEshuSearchIndexBM25ScoreStableAcrossPlansLive(t *testing.T) {
	database, ctx := openSearchIndexPartitionProofDB(t)
	conn, _ := searchIndexPartitionProofConn(t, ctx, database)
	defer func() { _ = conn.Close() }()

	createBM25PostingProofSchema(t, ctx, conn)
	seedBM25PlanStabilityRows(t, ctx, conn)

	search := indexstore.EshuSearchIndexSearch{
		ScopeID: "scope-bm25-plan",
		RepoID:  "repo-bm25-plan",
		Query:   "alpha beta gamma",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-bm25-plan"},
		Limit:   31,
	}
	terms, _ := indexstore.SortedSearchIndexTerms(searchhybrid.QueryTerms(search.Query))
	if len(terms) < 3 {
		t.Fatalf("plan-stability query tokenized to %d terms, want at least 3", len(terms))
	}

	planSettings := []struct {
		name string
		set  string
	}{
		{name: "default", set: ""},
		{name: "no-sort", set: "SET enable_sort=off"},
		{name: "no-nestloop-no-hashagg", set: "SET enable_nestloop=off; SET enable_hashagg=off"},
	}
	var baseline string
	for _, plan := range planSettings {
		if _, err := conn.ExecContext(ctx, "RESET enable_sort; RESET enable_nestloop; RESET enable_hashagg"); err != nil {
			t.Fatalf("reset planner settings: %v", err)
		}
		if plan.set != "" {
			if _, err := conn.ExecContext(ctx, plan.set); err != nil {
				t.Fatalf("apply planner settings %q: %v", plan.name, err)
			}
		}
		got := runBM25PlanStabilitySearch(t, ctx, conn, search)
		t.Logf("plan %s: %d candidates, %d bytes of score identity", plan.name, got.candidates, len(got.identity))
		if got.candidates == 0 {
			t.Fatalf("plan %s returned no candidates for the three-term query", plan.name)
		}
		if baseline == "" {
			baseline = got.identity
			continue
		}
		if got.identity != baseline {
			t.Fatalf("plan %s output differs from the default plan:\ndefault: %s\n%s: %s",
				plan.name, baseline, plan.name, got.identity)
		}
	}
}

type bm25PlanStabilityResult struct {
	candidates int
	identity   string
}

// runBM25PlanStabilitySearch runs the production persisted-index search on the
// proof connection and renders the output order plus exact score bits as one
// comparable string of document_id:score_bits entries.
func runBM25PlanStabilitySearch(
	t *testing.T,
	ctx context.Context,
	conn *sql.Conn,
	search indexstore.EshuSearchIndexSearch,
) bm25PlanStabilityResult {
	t.Helper()
	result, err := indexstore.NewEshuSearchIndexStore(searchIndexSQLConn{conn: conn}).Search(ctx, search)
	if err != nil {
		t.Fatalf("search BM25 plan stability: %v", err)
	}
	entries := make([]string, 0, len(result.Candidates))
	for i, candidate := range result.Candidates {
		if candidate.Score <= 0 {
			t.Fatalf("candidate[%d] score = %v, want > 0", i, candidate.Score)
		}
		entries = append(entries, fmt.Sprintf("%s:%d", candidate.Document.ID, math.Float64bits(candidate.Score)))
	}
	return bm25PlanStabilityResult{candidates: len(entries), identity: strings.Join(entries, "|")}
}

// seedBM25PlanStabilityRows seeds a corpus where every document matches at
// least three of five query terms with widely varying term frequencies and
// document lengths, so the per-document score is a multi-term float sum whose
// bits expose plan-dependent summation order without the ordered aggregate.
func seedBM25PlanStabilityRows(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()
	const (
		scopeID      = "scope-bm25-plan"
		generationID = "gen-bm25-plan"
		repoID       = "repo-bm25-plan"
		docCount     = 60
	)
	terms := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO ingestion_scopes(scope_id, active_generation_id)
VALUES ('scope-bm25-plan', 'gen-bm25-plan');
INSERT INTO eshu_search_index_stats(scope_id, generation_id, document_count, average_document_length)
VALUES ('scope-bm25-plan', 'gen-bm25-plan', 60, 200.0);
`); err != nil {
		t.Fatalf("seed BM25 plan-stability scopes/stats: %v", err)
	}
	for doc := 0; doc < docCount; doc++ {
		id := fmt.Sprintf("plandoc-%03d", doc)
		length := 50 + (doc*37)%400
		payload, err := json.Marshal(searchIndexDocumentFixture(id, repoID, "Plan stability runbook"))
		if err != nil {
			t.Fatalf("marshal BM25 plan-stability doc: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `
INSERT INTO eshu_search_index_documents(scope_id, generation_id, document_id, repo_id, source_kind, document, document_length)
VALUES ('scope-bm25-plan', 'gen-bm25-plan', $1, 'repo-bm25-plan', $2, $3::jsonb, $4)
`, id, string(searchdocs.SourceKindRuntimeSummary), string(payload), length); err != nil {
			t.Fatalf("insert BM25 plan-stability doc %s: %v", id, err)
		}
		for j, term := range terms {
			if (doc+j)%5 == 4 {
				continue
			}
			insertBM25TermLive(t, ctx, conn, scopeID, generationID, id, term, 1+(doc*7+j*13)%9)
		}
	}
	if _, err := conn.ExecContext(ctx, "VACUUM (ANALYZE) eshu_search_index_documents"); err != nil {
		t.Fatalf("vacuum BM25 plan-stability documents: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM (ANALYZE) eshu_search_index_terms"); err != nil {
		t.Fatalf("vacuum BM25 plan-stability terms: %v", err)
	}
}
