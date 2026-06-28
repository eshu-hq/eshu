// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// resetSemanticSearchInstrumentsForTest rebinds the lazily registered degraded
// counter so a test can register it against its own meter provider regardless of
// test ordering (mirrors resetAPIRequestMetricsForTest).
func resetSemanticSearchInstrumentsForTest() {
	semanticSearchInstrumentsOnce = sync.Once{}
	searchHybridDegradedTotal = nil
}

// degradedCounterValue sums the eshu_dp_search_hybrid_degraded_total datapoints
// whose attributes include every key/value in want (extra attributes such as
// service.namespace are ignored).
func degradedCounterValue(t *testing.T, rm metricdata.ResourceMetrics, want map[string]string) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_search_hybrid_degraded_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				match := true
				for k, v := range want {
					got, ok := dp.Attributes.Value(attribute.Key(k))
					if !ok || got.AsString() != v {
						match = false
						break
					}
				}
				if match {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// withDegradedCounterReader installs a process-global manual-reader meter provider
// and resets the lazily registered counter so the test observes its own datapoints.
func withDegradedCounterReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	resetSemanticSearchInstrumentsForTest()
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		resetSemanticSearchInstrumentsForTest()
		_ = provider.Shutdown(context.Background())
	})
	return reader
}

func collectDegradedMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

// TestSemanticSearchHandlerEmitsDegradedCounterOnHybridFallback proves a hybrid
// request served without semantic ranking (retrieval_state hybrid_degraded)
// increments the bounded degraded counter with query_type=hybrid, reason=no_embedder.
func TestSemanticSearchHandlerEmitsDegradedCounterOnHybridFallback(t *testing.T) {
	reader := withDegradedCounterReader(t)
	index := &fakeSemanticSearchIndexStore{
		result: semanticSearchIndexResult{
			IndexedDocumentCount: 1,
			RetrievalState:       "hybrid_degraded",
			Candidates: []searchretrieval.Candidate{{
				Document: semanticSearchDocumentFixture(
					"searchdoc:p", "repo-p", "Payments", "payment runbook",
				),
				Score:    1.0,
				Metadata: map[string]string{"search_method": "bm25"},
			}},
		},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, semanticSearchHTTPRequest(t, map[string]any{
		"repo_id": "repo-p", "query": "payment", "mode": "hybrid", "limit": 1, "timeout_ms": 250,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	rm := collectDegradedMetrics(t, reader)
	if got := degradedCounterValue(t, rm, map[string]string{"query_type": "hybrid", "reason": "no_embedder"}); got != 1 {
		t.Fatalf("degraded counter = %d, want 1", got)
	}
}

// TestSemanticSearchHandlerDoesNotEmitDegradedOnActiveHybrid proves a fully
// active hybrid run (retrieval_state hybrid_active) does NOT increment the counter.
func TestSemanticSearchHandlerDoesNotEmitDegradedOnActiveHybrid(t *testing.T) {
	reader := withDegradedCounterReader(t)
	index := &fakeSemanticSearchIndexStore{
		result: semanticSearchIndexResult{
			IndexedDocumentCount: 1,
			RetrievalState:       "hybrid_active",
			Candidates: []searchretrieval.Candidate{{
				Document: semanticSearchDocumentFixture(
					"searchdoc:p", "repo-p", "Payments", "payment runbook",
				),
				Score:    1.0,
				Metadata: map[string]string{"search_method": "rrf_hybrid"},
			}},
		},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, semanticSearchHTTPRequest(t, map[string]any{
		"repo_id": "repo-p", "query": "payment", "mode": "hybrid", "limit": 1, "timeout_ms": 250,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	rm := collectDegradedMetrics(t, reader)
	if got := degradedCounterValue(t, rm, map[string]string{}); got != 0 {
		t.Fatalf("degraded counter = %d, want 0 (active hybrid is not degraded)", got)
	}
}

// TestSemanticSearchHandlerEmitsDegradedOnSemanticNoEmbedder proves the
// semantic-mode no-embedder 503 path also increments the degraded counter with
// query_type=semantic, reason=no_embedder.
func TestSemanticSearchHandlerEmitsDegradedOnSemanticNoEmbedder(t *testing.T) {
	reader := withDegradedCounterReader(t)
	// No LocalHybrid configured, so semantic mode is refused with the
	// embedder-unavailable error (AllowSemantic=false on the persisted backend).
	handler := &SemanticSearchHandler{Index: &fakeSemanticSearchIndexStore{}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, semanticSearchHTTPRequest(t, map[string]any{
		"repo_id": "repo-p", "query": "payment", "mode": "semantic", "limit": 1, "timeout_ms": 250,
	}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}

	rm := collectDegradedMetrics(t, reader)
	if got := degradedCounterValue(t, rm, map[string]string{"query_type": "semantic", "reason": "no_embedder"}); got != 1 {
		t.Fatalf("degraded counter = %d, want 1", got)
	}
}

// TestSemanticSearchDegradation maps every retrieval_state the handler can emit
// to the bounded degraded-signal classification. Only states where the caller
// asked for semantic ranking (hybrid/semantic) but did not get it count as
// degraded; an explicit keyword request and a fully active hybrid/semantic run
// are not degradations.
func TestSemanticSearchDegradation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		retrievalState string
		wantDegraded   bool
		wantQueryType  string
		wantReason     string
	}{
		{"hybrid degraded to bm25", "hybrid_degraded", true, "hybrid", "no_embedder"},
		{"semantic unavailable", "semantic_unavailable", true, "semantic", "no_embedder"},
		{"hybrid active", "hybrid_active", false, "", ""},
		{"semantic active", "semantic_active", false, "", ""},
		{"keyword requested", "keyword_only", false, "", ""},
		{"empty state", "", false, "", ""},
		{"unknown state", "something_else", false, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			degraded, queryType, reason := semanticSearchDegradation(tc.retrievalState)
			if degraded != tc.wantDegraded {
				t.Fatalf("degraded = %v, want %v", degraded, tc.wantDegraded)
			}
			if queryType != tc.wantQueryType {
				t.Fatalf("queryType = %q, want %q", queryType, tc.wantQueryType)
			}
			if reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
