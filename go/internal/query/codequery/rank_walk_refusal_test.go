// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

// TestCodeRankWalkPageWithAMissingRowIsAFixedServerError drives the
// page-shaping step with a page id that has no row: 500 with the fixed
// message, no row data, no rank_walk block, and no walk outcome.
func TestCodeRankWalkPageWithAMissingRowIsAFixedServerError(t *testing.T) {
	t.Parallel()
	handler, _ := walkCodeHandler(1, nil)
	byID := map[string]map[string]any{"a": {"entity_id": "a", "source_cache": "secret body"}}
	page := rank.Page{Lane: rank.LaneContent, Order: rank.OrderHybrid, IDs: []string{"a", "ghost"}, Position: 2, WindowRows: 2}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", nil).WithContext(ctx)
	handler.writeRankWalkPage(rec, req, codeRankWalkRequest{query: "q", repoID: "repo-a", limit: 2}, page, byID, map[string]float64{"a": 1}, "code_search.fuzzy_symbol")
	span.End()
	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body, codeSearchRankFailedMessage) {
		t.Fatalf("status = %d body = %s, want 500 %q", rec.Code, body, codeSearchRankFailedMessage)
	}
	for _, leak := range []string{"rank_walk", "secret body", "ghost"} {
		if strings.Contains(body, leak) {
			t.Fatalf("body = %s, leaks %q", body, leak)
		}
	}
	for _, ended := range recorder.Ended() {
		for _, kv := range ended.Attributes() {
			if kv.Key == rank.AttrOutcome {
				t.Fatalf("span recorded %s=%s; a missing row is a server fault, not a walk outcome", kv.Key, kv.Value.AsString())
			}
		}
	}
}
