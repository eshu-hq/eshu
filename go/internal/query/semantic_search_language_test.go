// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// TestSemanticSearchHandlerLanguageFilterNarrowsResults verifies that supplying
// languages=["go"] restricts what the index query receives and that the response
// carries facet counts for the returned documents.
func TestSemanticSearchHandlerLanguageFilterNarrowsResults(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{
		result: semanticSearchIndexResult{
			IndexedDocumentCount: 2,
			Candidates: []searchretrieval.Candidate{
				{
					Document: semanticSearchDocumentWithLanguageFixture("doc:go-service", "repo-1", "go"),
					Score:    2.0,
					Metadata: map[string]string{"search_method": "bm25"},
				},
			},
		},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-1",
		"query":      "service",
		"mode":       "keyword",
		"limit":      10,
		"timeout_ms": 250,
		"languages":  []string{"go"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(index.query.Languages), 1; got != want {
		t.Fatalf("query.Languages len = %d, want %d", got, want)
	}
	if got, want := index.query.Languages[0], "go"; got != want {
		t.Fatalf("query.Languages[0] = %q, want %q", got, want)
	}

	data := semanticSearchEnvelopeData(t, rec)

	// Facets must be present and contain the go language count.
	facets := mustMapField(t, data, "facets")
	langs := mustMapField(t, facets, "languages")
	if got, ok := langs["go"]; !ok || got != float64(1) {
		t.Fatalf("facets.languages[go] = %v (ok=%v), want 1", got, ok)
	}
}

// TestSemanticSearchHandlerUnknownLanguageReturns400 verifies that an
// unrecognised language value is rejected before any index read.
func TestSemanticSearchHandlerUnknownLanguageReturns400(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-1",
		"query":      "service",
		"mode":       "keyword",
		"limit":      10,
		"timeout_ms": 250,
		"languages":  []string{"not_a_real_language_xyz"},
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if index.calls != 0 {
		t.Fatalf("index calls = %d, want 0 for unknown language", index.calls)
	}
	if !strings.Contains(rec.Body.String(), "languages") {
		t.Fatalf("body = %s, want error mentioning languages", rec.Body.String())
	}
}

// TestSemanticSearchHandlerFacetsAlwaysPresentEvenWithoutFilter checks that the
// facets block is always present in the response, even when no languages filter
// is requested, and that it counts all languages in the result set.
func TestSemanticSearchHandlerFacetsAlwaysPresentEvenWithoutFilter(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{
		result: semanticSearchIndexResult{
			IndexedDocumentCount: 3,
			Candidates: []searchretrieval.Candidate{
				{
					Document: semanticSearchDocumentWithLanguageFixture("doc:go-1", "repo-1", "go"),
					Score:    3.0,
					Metadata: map[string]string{"search_method": "bm25"},
				},
				{
					Document: semanticSearchDocumentWithLanguageFixture("doc:python-1", "repo-1", "python"),
					Score:    2.0,
					Metadata: map[string]string{"search_method": "bm25"},
				},
				{
					Document: semanticSearchDocumentWithLanguageFixture("doc:go-2", "repo-1", "go"),
					Score:    1.0,
					Metadata: map[string]string{"search_method": "bm25"},
				},
			},
		},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-1",
		"query":      "service",
		"mode":       "keyword",
		"limit":      10,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	data := semanticSearchEnvelopeData(t, rec)
	facets, ok := data["facets"]
	if !ok {
		t.Fatal("response missing facets field")
	}
	facetsMap, ok := facets.(map[string]any)
	if !ok {
		t.Fatalf("facets = %T, want map[string]any", facets)
	}
	langs, ok := facetsMap["languages"].(map[string]any)
	if !ok {
		t.Fatalf("facets.languages = %T, want map[string]any", facetsMap["languages"])
	}
	if got, want := langs["go"], float64(2); got != want {
		t.Fatalf("facets.languages[go] = %v, want %v", got, want)
	}
	if got, want := langs["python"], float64(1); got != want {
		t.Fatalf("facets.languages[python] = %v, want %v", got, want)
	}
}

// TestSemanticSearchHandlerLanguagesNormalisedLowercase verifies that language
// values from the request are lowercased before validation and index query.
func TestSemanticSearchHandlerLanguagesNormalisedLowercase(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-1",
		"query":      "service",
		"mode":       "keyword",
		"limit":      10,
		"timeout_ms": 250,
		"languages":  []string{"GO"},
	})
	rec := httptest.NewRecorder()
	handler.search(rec, req)

	// "GO" normalises to "go" which is valid, so it should succeed (not 400).
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := index.query.Languages[0], "go"; got != want {
		t.Fatalf("normalised language = %q, want %q", got, want)
	}
}

// TestSemanticSearchHandlerEmptyLanguagesSliceIsNoOp verifies that sending an
// empty languages list results in no language filter on the index query.
func TestSemanticSearchHandlerEmptyLanguagesSliceIsNoOp(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-1",
		"query":      "service",
		"mode":       "keyword",
		"limit":      10,
		"timeout_ms": 250,
		"languages":  []string{},
	})
	rec := httptest.NewRecorder()
	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(index.query.Languages) != 0 {
		t.Fatalf("query.Languages = %v, want empty for no-op", index.query.Languages)
	}
}

// TestSemanticSearchHandlerPassesLanguagesToIndex verifies that languages
// propagate end-to-end into the index query alongside source_kinds.
func TestSemanticSearchHandlerPassesLanguagesToIndex(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":      "repo-1",
		"query":        "service",
		"mode":         "keyword",
		"limit":        10,
		"timeout_ms":   250,
		"source_kinds": []string{"code_entity"},
		"languages":    []string{"typescript", "javascript"},
	})
	rec := httptest.NewRecorder()
	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(index.query.Languages), 2; got != want {
		t.Fatalf("query.Languages len = %d, want %d", got, want)
	}
	if got, want := len(index.query.SourceKinds), 1; got != want {
		t.Fatalf("query.SourceKinds len = %d, want %d", got, want)
	}
	if got, want := index.query.SourceKinds[0], searchdocs.SourceKindCodeEntity; got != want {
		t.Fatalf("query.SourceKinds[0] = %q, want %q", got, want)
	}
}

// semanticSearchDocumentWithLanguageFixture builds a document fixture whose
// Labels carry the given language label.
func semanticSearchDocumentWithLanguageFixture(id, repoID, lang string) searchdocs.Document {
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	return searchdocs.Document{
		ID:          id,
		RepoID:      repoID,
		SourceKind:  searchdocs.SourceKindCodeEntity,
		Title:       "Entity " + id,
		Path:        "pkg/main.go",
		ContextText: "sample context",
		UpdatedAt:   now,
		TruthScope: searchdocs.TruthScope{
			Level: searchdocs.TruthLevelDerived,
			Basis: searchdocs.TruthBasisContentIndex,
		},
		Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		AccessScope: searchdocs.AccessScope{RepoID: repoID},
		GraphHandles: []searchdocs.GraphHandle{
			{Kind: "repository", ID: repoID},
		},
		Labels: []string{"language:" + lang},
	}
}
