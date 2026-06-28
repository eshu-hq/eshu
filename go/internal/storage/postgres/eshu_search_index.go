// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

const eshuSearchIndexDefaultLimit = 100

const eshuSearchIndexStatsQuery = `
WITH active_generation AS (
    SELECT active_generation_id AS generation_id
    FROM ingestion_scopes
    WHERE scope_id = $1
      AND active_generation_id IS NOT NULL
)
SELECT stats.document_count, false AS corpus_may_be_truncated
FROM eshu_search_index_stats stats
JOIN active_generation active ON active.generation_id = stats.generation_id
WHERE stats.scope_id = $1
`

// EshuSearchIndexSearch is one bounded persisted-index search over active
// curated search documents.
type EshuSearchIndexSearch struct {
	ScopeID     string
	RepoID      string
	Query       string
	Anchor      searchretrieval.Anchor
	SourceKinds []searchdocs.SourceKind
	// Languages filters the corpus to documents whose Labels array contains
	// a "language:<lang>" entry for one of the requested languages. An empty
	// slice means no language filter.
	Languages []string
	Limit     int
}

// EshuSearchIndexSearchResult is the ranked candidate page plus corpus metadata
// read from the persisted index.
type EshuSearchIndexSearchResult struct {
	Candidates           []searchretrieval.Candidate
	IndexedDocumentCount int
	CorpusMayBeTruncated bool
}

// EshuSearchIndexStore reads the persisted BM25 index for active curated search
// documents.
type EshuSearchIndexStore struct {
	db ExecQueryer
}

// NewEshuSearchIndexStore builds a persisted search-index reader over db.
func NewEshuSearchIndexStore(db ExecQueryer) EshuSearchIndexStore {
	return EshuSearchIndexStore{db: db}
}

// Search ranks active search documents using persisted BM25 postings. It joins
// the requested scope's active generation before scoring, so superseded
// generation postings are ignored without query-time rebuilds.
func (s EshuSearchIndexStore) Search(
	ctx context.Context,
	search EshuSearchIndexSearch,
) (EshuSearchIndexSearchResult, error) {
	if s.db == nil {
		return EshuSearchIndexSearchResult{}, fmt.Errorf("eshu search index database is required")
	}
	search = normalizeEshuSearchIndexSearch(search)
	if err := validateEshuSearchIndexSearch(search); err != nil {
		return EshuSearchIndexSearchResult{}, err
	}
	result, err := s.loadStats(ctx, search.ScopeID)
	if err != nil {
		return EshuSearchIndexSearchResult{}, err
	}
	terms, termKeys := sortedSearchIndexTerms(searchhybrid.QueryTerms(search.Query))
	if len(terms) == 0 {
		return result, nil
	}

	query, args := buildEshuSearchIndexQuery(search, terms, termKeys)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return EshuSearchIndexSearchResult{}, fmt.Errorf("search persisted eshu search index: %w", err)
	}
	defer func() { _ = rows.Close() }()

	rank := 0
	for rows.Next() {
		var payload []byte
		var score float64
		var documentCount int64
		var corpusMayBeTruncated bool
		if err := rows.Scan(&payload, &score, &documentCount, &corpusMayBeTruncated); err != nil {
			return EshuSearchIndexSearchResult{}, fmt.Errorf("scan persisted eshu search result: %w", err)
		}
		var doc searchdocs.Document
		if err := json.Unmarshal(payload, &doc); err != nil {
			return EshuSearchIndexSearchResult{}, fmt.Errorf("decode persisted eshu search document: %w", err)
		}
		rank++
		result.IndexedDocumentCount = int(documentCount)
		result.CorpusMayBeTruncated = corpusMayBeTruncated
		result.Candidates = append(result.Candidates, searchretrieval.Candidate{
			Document: doc,
			Score:    score,
			Metadata: map[string]string{
				"search_method": "bm25",
				"bm25_rank":     fmt.Sprintf("%d", rank),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return EshuSearchIndexSearchResult{}, fmt.Errorf("iterate persisted eshu search results: %w", err)
	}
	return result, nil
}

func (s EshuSearchIndexStore) loadStats(
	ctx context.Context,
	scopeID string,
) (EshuSearchIndexSearchResult, error) {
	rows, err := s.db.QueryContext(ctx, eshuSearchIndexStatsQuery, scopeID)
	if err != nil {
		return EshuSearchIndexSearchResult{}, fmt.Errorf("load eshu search index stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result EshuSearchIndexSearchResult
	if rows.Next() {
		var documentCount int64
		var corpusMayBeTruncated bool
		if err := rows.Scan(&documentCount, &corpusMayBeTruncated); err != nil {
			return EshuSearchIndexSearchResult{}, fmt.Errorf("scan eshu search index stats: %w", err)
		}
		result.IndexedDocumentCount = int(documentCount)
		result.CorpusMayBeTruncated = corpusMayBeTruncated
	}
	if err := rows.Err(); err != nil {
		return EshuSearchIndexSearchResult{}, fmt.Errorf("iterate eshu search index stats: %w", err)
	}
	return result, nil
}

func normalizeEshuSearchIndexSearch(search EshuSearchIndexSearch) EshuSearchIndexSearch {
	search.ScopeID = strings.TrimSpace(search.ScopeID)
	search.RepoID = strings.TrimSpace(search.RepoID)
	search.Query = strings.TrimSpace(search.Query)
	search.Anchor.ID = strings.TrimSpace(search.Anchor.ID)
	if search.Limit <= 0 {
		search.Limit = eshuSearchIndexDefaultLimit
	}
	for i, kind := range search.SourceKinds {
		search.SourceKinds[i] = searchdocs.SourceKind(strings.TrimSpace(string(kind)))
	}
	for i, lang := range search.Languages {
		search.Languages[i] = strings.TrimSpace(lang)
	}
	return search
}

func validateEshuSearchIndexSearch(search EshuSearchIndexSearch) error {
	var problems []error
	if search.ScopeID == "" {
		problems = append(problems, errors.New("eshu search index search requires a scope id"))
	}
	if search.RepoID == "" {
		problems = append(problems, errors.New("eshu search index search requires a repo id"))
	}
	if search.Query == "" {
		problems = append(problems, errors.New("eshu search index search requires a query"))
	}
	if search.Anchor.Kind == "" || search.Anchor.ID == "" {
		problems = append(problems, errors.New("eshu search index search requires an anchor"))
	}
	return errors.Join(problems...)
}

func sortedSearchIndexTerms(counts map[string]int) ([]string, []string) {
	terms := make([]string, 0, len(counts))
	for term := range counts {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	termKeys := make([]string, 0, len(terms))
	for _, term := range terms {
		termKeys = append(termKeys, searchhybrid.TermKey(term))
	}
	return terms, termKeys
}

func buildEshuSearchIndexQuery(search EshuSearchIndexSearch, terms []string, termKeys []string) (string, []any) {
	args := []any{search.ScopeID, terms, termKeys, search.RepoID, int64(search.Limit)}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	conditions := []string{"d.repo_id = $4"}
	switch search.Anchor.Kind {
	case searchretrieval.ScopeKindRepo:
		conditions = append(conditions, searchIndexHandlePredicate(addArg, "repository", search.Anchor.ID, true))
	case searchretrieval.ScopeKindService:
		conditions = append(conditions, searchIndexHandlePredicate(addArg, "service", search.Anchor.ID, false))
	case searchretrieval.ScopeKindWorkload:
		conditions = append(conditions, searchIndexHandlePredicate(addArg, "workload", search.Anchor.ID, false))
	case searchretrieval.ScopeKindEnvironment:
		conditions = append(conditions, searchIndexHandlePredicate(addArg, "environment", search.Anchor.ID, false))
	}
	if len(search.SourceKinds) > 0 {
		kinds := make([]string, 0, len(search.SourceKinds))
		for _, kind := range search.SourceKinds {
			if kind != "" {
				kinds = append(kinds, string(kind))
			}
		}
		if len(kinds) > 0 {
			conditions = append(conditions, "d.source_kind = ANY("+addArg(kinds)+"::text[])")
		}
	}
	if len(search.Languages) > 0 {
		langs := make([]string, 0, len(search.Languages))
		for _, lang := range search.Languages {
			if lang != "" {
				langs = append(langs, "language:"+lang)
			}
		}
		if len(langs) > 0 {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM jsonb_array_elements_text(d.document->'Labels') AS lbl WHERE lbl = ANY("+addArg(langs)+"::text[]))")
		}
	}

	var builder strings.Builder
	builder.WriteString(`
WITH active_generation AS (
    SELECT active_generation_id AS generation_id
    FROM ingestion_scopes
    WHERE scope_id = $1
      AND active_generation_id IS NOT NULL
),
query_terms AS (
    SELECT term, term_key
    FROM unnest($2::text[], $3::text[]) AS q(term, term_key)
),
document_frequency AS (
    SELECT t.term_key, t.term, count(*)::float8 AS doc_frequency
    FROM eshu_search_index_terms t
    JOIN active_generation active ON active.generation_id = t.generation_id
    JOIN query_terms q ON q.term_key = t.term_key AND q.term = t.term
    WHERE t.scope_id = $1
    GROUP BY t.term_key, t.term
),
scored AS (
    SELECT
        d.document,
        d.document_id,
        stats.document_count,
        false AS corpus_may_be_truncated,
        SUM(
            LN(1 + ((stats.document_count::float8 - df.doc_frequency + 0.5) / (df.doc_frequency + 0.5))) *
            ((t.term_frequency::float8 * 2.2) /
             (t.term_frequency::float8 + 1.2 * (0.25 + 0.75 * (d.document_length::float8 / NULLIF(stats.average_document_length, 0)))))
        ) AS score
    FROM eshu_search_index_terms t
    JOIN active_generation active ON active.generation_id = t.generation_id
    JOIN query_terms q ON q.term_key = t.term_key AND q.term = t.term
    JOIN document_frequency df ON df.term_key = t.term_key AND df.term = t.term
    JOIN eshu_search_index_documents d
      ON d.scope_id = t.scope_id
     AND d.generation_id = t.generation_id
     AND d.document_id = t.document_id
    JOIN eshu_search_index_stats stats
      ON stats.scope_id = t.scope_id
     AND stats.generation_id = t.generation_id
    WHERE t.scope_id = $1
      AND `)
	builder.WriteString(strings.Join(conditions, "\n      AND "))
	builder.WriteString(`
    GROUP BY d.document, d.document_id, stats.document_count
)
SELECT document, score, document_count, corpus_may_be_truncated
FROM scored
WHERE score > 0
ORDER BY score DESC, document_id ASC
LIMIT $5
`)
	return builder.String(), args
}

func searchIndexHandlePredicate(addArg func(any) string, kind string, id string, allowDirectRepo bool) string {
	kindArg := addArg(kind)
	idArg := addArg(id)
	exists := fmt.Sprintf(`EXISTS (
        SELECT 1
        FROM jsonb_array_elements(d.document->'GraphHandles') AS handle
        WHERE handle->>'Kind' = %s
          AND handle->>'ID' = %s
    )`, kindArg, idArg)
	if allowDirectRepo {
		return "(d.repo_id = " + idArg + " OR " + exists + ")"
	}
	return exists
}
