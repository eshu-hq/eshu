// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeSearchVectorReadyReader reports a fixed SearchVectorReadyFreshness for
// every probe, so tests can pin the handler's freshness downgrade without a
// live Postgres watermark.
type fakeSearchVectorReadyReader struct {
	freshness SearchVectorReadyFreshness
	err       error
	calls     int
}

func (f *fakeSearchVectorReadyReader) SearchVectorReadyWatermark(context.Context) (SearchVectorReadyFreshness, error) {
	f.calls++
	return f.freshness, f.err
}

// TestSemanticSearchHandlerKeywordModeIgnoresPendingSearchVector proves an
// explicit mode:"keyword" request is served fresh even when the search-vector
// build sweep has never published search_vector_ready: the keyword path is
// the deterministic lexical index and is never degraded by vector/index
// readiness (#4673 review fix — the vector-freshness downgrade must be gated
// to vector-backed modes).
func TestSemanticSearchHandlerKeywordModeIgnoresPendingSearchVector(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{result: semanticSearchIndexResult{RetrievalState: "keyword_only"}}
	ready := &fakeSearchVectorReadyReader{freshness: SearchVectorReadyFreshness{Signaled: true, Present: false}}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction, SearchVectorReady: ready}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      1,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	envelope := decodeSemanticSearchEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatal("truth envelope = nil, want search truth")
	}
	if got, want := envelope.Truth.Freshness.State, FreshnessFresh; got != want {
		t.Fatalf("keyword mode freshness state = %q, want %q (must not be downgraded by a pending search-vector build)", got, want)
	}
	if envelope.Truth.Freshness.Cause != "" {
		t.Fatalf("keyword mode freshness cause = %q, want empty", envelope.Truth.Freshness.Cause)
	}
}

// TestSemanticSearchHandlerHybridModeAppliesPendingSearchVector proves an
// explicit mode:"hybrid" (vector-backed) request IS downgraded with the
// pending_search_vector cause under the same never-published watermark that
// keyword mode ignores.
func TestSemanticSearchHandlerHybridModeAppliesPendingSearchVector(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{result: semanticSearchIndexResult{RetrievalState: "hybrid_active"}}
	ready := &fakeSearchVectorReadyReader{freshness: SearchVectorReadyFreshness{Signaled: true, Present: false}}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction, SearchVectorReady: ready}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "hybrid",
		"limit":      1,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	envelope := decodeSemanticSearchEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatal("truth envelope = nil, want search truth")
	}
	if got, want := envelope.Truth.Freshness.State, FreshnessBuilding; got != want {
		t.Fatalf("hybrid mode freshness state = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Freshness.Cause, FreshnessCausePendingSearchVector; got != want {
		t.Fatalf("hybrid mode freshness cause = %q, want %q", got, want)
	}
	if ready.calls != 1 {
		t.Fatalf("ready.calls = %d, want 1", ready.calls)
	}
}

func decodeSemanticSearchEnvelope(t *testing.T, rec *httptest.ResponseRecorder) ResponseEnvelope {
	t.Helper()
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	return envelope
}
