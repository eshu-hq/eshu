// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

// TestEntityRankWalkPageWithAMissingRowIsAFixedServerError drives the
// page-shaping step with a page id that has no row. The route must answer
// 500 with the fixed message, no row data, no rank_walk block, and no served
// outcome: a short page would count the missing row as served and lose it.
func TestEntityRankWalkPageWithAMissingRowIsAFixedServerError(t *testing.T) {
	t.Parallel()
	h := &ContentHandler{Profile: querycontract.ProfileLocalAuthoritative}
	rows := []querycontract.EntityContent{{EntityID: "a", RepoID: walkRepoID, SourceCache: "secret body"}}
	page := rank.Page{Lane: rank.LaneContent, Order: rank.OrderHybrid, IDs: []string{"a", "ghost"}, Position: 2, WindowRows: 2}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/entities/search", nil).WithContext(ctx)
	h.writeEntityRankWalkPage(rec, req, contentSearchRequest{Query: "q", RepoID: walkRepoID, Limit: 2}, rows, map[string]float64{"a": 1}, page)
	span.End()
	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body, contentEntityRankFailedMessage) {
		t.Fatalf("status = %d body = %s, want 500 %q", rec.Code, body, contentEntityRankFailedMessage)
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

// TestEntityPageRowsRefusesAnIDWithNoRow mirrors rank.MapPage: a page id
// with no row is refused (no rows, ErrPageRowMissing), never a short page.
func TestEntityPageRowsRefusesAnIDWithNoRow(t *testing.T) {
	t.Parallel()
	rows := []querycontract.EntityContent{{EntityID: "a"}}
	page, err := entityPageRows(rows, map[string]float64{"a": 1}, []string{"ghost", "a"})
	if !errors.Is(err, rank.ErrPageRowMissing) || page != nil {
		t.Fatalf("entityPageRows = %+v, %v; want no rows and ErrPageRowMissing", page, err)
	}
}
