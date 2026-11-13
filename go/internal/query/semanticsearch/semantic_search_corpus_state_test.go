// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func newSemanticSearchCorpusStateMux(handler *SemanticSearchHandler) *http.ServeMux {
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func TestSemanticSearchCorpusStateDerivation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		count       int
		mayTruncate bool
		resultCount int
		want        string
	}{
		{name: "zero count reports empty", count: 0, want: "empty"},
		{name: "zero count wins over truncation", count: 0, mayTruncate: true, want: "empty"},
		{name: "positive count with truncation reports truncated", count: 5, mayTruncate: true, want: "truncated"},
		{name: "positive count with truncation and results reports truncated", count: 5, mayTruncate: true, resultCount: 3, want: "truncated"},
		{name: "positive count without truncation reports populated", count: 5, resultCount: 3, want: "populated"},
		{name: "positive count with no results reports populated", count: 5, want: "populated"},
		{name: "results beside zero count reports populated", count: 0, resultCount: 2, want: "populated"},
		{name: "negative count never reports empty", count: -2, want: "populated"},
		{name: "negative count with truncation never reports empty", count: -2, mayTruncate: true, want: "truncated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := semanticSearchCorpusState(tc.count, tc.mayTruncate, tc.resultCount); got != tc.want {
				t.Fatalf("semanticSearchCorpusState(%d, %t, %d) = %q, want %q",
					tc.count, tc.mayTruncate, tc.resultCount, got, tc.want)
			}
		})
	}
}

func TestSemanticSearchCorpusStateIsClosedEnum(t *testing.T) {
	t.Parallel()

	for count := -2; count <= 2; count++ {
		for _, mayTruncate := range []bool{false, true} {
			for results := 0; results <= 2; results++ {
				got := semanticSearchCorpusState(count, mayTruncate, results)
				switch got {
				case "empty", "truncated", "populated":
				default:
					t.Fatalf("semanticSearchCorpusState(%d, %t, %d) = %q, want one of empty/truncated/populated",
						count, mayTruncate, results, got)
				}
			}
		}
	}
}

func TestSemanticSearchHandlerEmptyCorpusReportsEmptyState(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{
		result: SemanticSearchIndexResult{IndexedDocumentCount: 0},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction}
	mux := newSemanticSearchCorpusStateMux(handler)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, testutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	}))

	if got, want := rec.Code, 200; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["corpus_state"], "empty"; got != want {
		t.Fatalf("corpus_state = %#v, want %#v", got, want)
	}
	if results := data["results"].([]any); len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0", len(results))
	}
	if got, want := data["retrieval_state"], "keyword_only"; got != want {
		t.Fatalf("retrieval_state = %#v, want %#v (untouched)", got, want)
	}
}

func TestSemanticSearchHandlerFilteredToZeroReportsPopulatedState(t *testing.T) {
	t.Parallel()

	// The persisted index reports the whole-scope count before
	// source-kind/language filters, so a populated corpus that matches
	// nothing reports populated with an empty result set.
	index := &fakeSemanticSearchIndexStore{
		result: SemanticSearchIndexResult{IndexedDocumentCount: 5},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction}

	rec := httptest.NewRecorder()
	handler.search(rec, testutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "nothing matches this",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	}))

	if got, want := rec.Code, 200; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["corpus_state"], "populated"; got != want {
		t.Fatalf("corpus_state = %#v, want %#v", got, want)
	}
	if results := data["results"].([]any); len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0", len(results))
	}
}

func TestSemanticSearchHandlerTruncatedCorpusReportsTruncatedState(t *testing.T) {
	t.Parallel()

	// The bounded corpus read may not cover the whole corpus, so the
	// production call-site mapping must surface truncated on the wire.
	// This pins the served body value end to end: swapping the call-site
	// args or hardcoding mayTruncate=false must fail this test.
	index := &fakeSemanticSearchIndexStore{
		result: SemanticSearchIndexResult{IndexedDocumentCount: 5, CorpusMayBeTruncated: true},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction}

	rec := httptest.NewRecorder()
	handler.search(rec, testutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	}))

	if got, want := rec.Code, 200; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["corpus_state"], "truncated"; got != want {
		t.Fatalf("corpus_state = %#v, want %#v", got, want)
	}
	if results := data["results"].([]any); len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0", len(results))
	}
}

func TestSemanticSearchHandlerZeroCountWithResultsReportsPopulated(t *testing.T) {
	t.Parallel()

	// A store that serves candidates while reporting a zero count
	// under-reports; the wire stays self-consistent by never printing
	// empty beside non-empty results.
	index := &fakeSemanticSearchIndexStore{
		result: SemanticSearchIndexResult{
			Candidates: []searchretrieval.Candidate{{
				Document: testutil.SemanticSearchDocumentFixture(
					"searchdoc:payments",
					"repo-payments",
					"Payments runbook",
					"payment runbook ownership escalation",
				),
				Score:    2.5,
				Metadata: map[string]string{"search_method": "bm25"},
			}},
		},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction}

	rec := httptest.NewRecorder()
	handler.search(rec, testutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	}))

	if got, want := rec.Code, 200; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["corpus_state"], "populated"; got != want {
		t.Fatalf("corpus_state = %#v, want %#v", got, want)
	}
	if results := data["results"].([]any); len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
}

func TestSemanticSearchHandlerScopedEmptyGrantReportsEmptyCorpusState(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	handler := &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction}
	req := testutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:        auth.AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, 200; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if index.calls != 0 {
		t.Fatalf("index calls = %d, want 0 for empty scoped grant", index.calls)
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["corpus_state"], "empty"; got != want {
		t.Fatalf("corpus_state = %#v, want %#v", got, want)
	}
}

func TestSemanticSearchHandlerCorpusStateSpanAttribute(t *testing.T) {
	// No t.Parallel: this test swaps the package-level semanticSearchTracer,
	// which parallel tests read in startQueryHandlerSpan. A parallel swap
	// races with those readers (race detector, pre-push floor).

	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := semanticSearchTracer
	semanticSearchTracer = provider.Tracer("semantic-search-corpus-state-test")
	t.Cleanup(func() { semanticSearchTracer = previous })

	index := &fakeSemanticSearchIndexStore{
		result: SemanticSearchIndexResult{IndexedDocumentCount: 0},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction}
	mux := newSemanticSearchCorpusStateMux(handler)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, testutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	}))

	if got, want := rec.Code, 200; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var requestSpan trace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == telemetry.SpanQuerySemanticSearch {
			requestSpan = span
		}
	}
	if requestSpan == nil {
		t.Fatalf("no %q span recorded", telemetry.SpanQuerySemanticSearch)
	}
	if got := semanticSearchSpanAttribute(requestSpan.Attributes(), "search.corpus_state"); got != "empty" {
		t.Fatalf("search.corpus_state = %q, want %q", got, "empty")
	}
	if got := semanticSearchSpanAttribute(requestSpan.Attributes(), "search.retrieval_state"); got != "keyword_only" {
		t.Fatalf("search.retrieval_state = %q, want %q (untouched)", got, "keyword_only")
	}
}
