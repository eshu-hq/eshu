// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestAnswerCodeRankRefusalAnswersAnUnlistedCode guards the find_code rank
// refusal path against a *rank.Error whose code rank.WriteError does not
// answer. rank.Answer then writes nothing and reports false. Before #7674
// slice 14 the refusal branches ignored that and returned, so the request
// ended as an empty 200. The helper must answer the fixed rank failure 500
// instead, with the error on the span and none of its text in the body.
func TestAnswerCodeRankRefusalAnswersAnUnlistedCode(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("rank-refusal-test").Start(context.Background(), "request")

	err := &rank.Error{Code: "rank_cursor_unlisted", Reason: "unlisted", Message: failure.Canary}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	answerCodeRankRefusal(rec, req, err, "code_search.fuzzy_symbol")
	span.End()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (an unanswered refusal must not end as an empty 200); body = %q", rec.Code, rec.Body.String())
	}
	if got := failure.Message(rec.Body.Bytes()); got != codeSearchRankFailedMessage {
		t.Errorf("body message = %q, want %q", got, codeSearchRankFailedMessage)
	}
	for _, token := range []string{"hunter2", "pg-primary.canary.internal", "canary_secret_table", "rank_cursor_unlisted"} {
		if strings.Contains(rec.Body.String(), token) {
			t.Errorf("body quotes %q: %s", token, rec.Body.String())
		}
	}
	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Status().Code != codes.Error {
		t.Fatalf("request span status = %v, want Error", ended)
	}
}
