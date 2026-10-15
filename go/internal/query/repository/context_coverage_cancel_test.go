// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// contextCoverageGraphFileCount is the file count the graph fallback answers
// when the context route's content coverage read fails.
const contextCoverageGraphFileCount = 42

// contextCoverageGraph answers the repository base lookup and the context
// count queries, with contextCoverageGraphFileCount for the file count.
func contextCoverageGraph() graph.FakeGraphReader {
	return graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repo-1", "name": "example"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "[:REPO_CONTAINS]->(f:File)"):
				return []map[string]any{{"count": int64(contextCoverageGraphFileCount)}}, nil
			case strings.Contains(cypher, "RETURN count("):
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return nil, nil
		},
	}
}

// serveContextCoverageFailure serves GET .../context with the content
// coverage read failing with err, on a recorded span, canceling the request
// context first when cancel is set.
func serveContextCoverageFailure(t *testing.T, err error, cancel bool) (*httptest.ResponseRecorder, sdktrace.ReadOnlySpan, string) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("repository-context-coverage-test").Start(context.Background(), "http")
	ctx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()
	if cancel {
		cancelCtx()
	}

	var logs bytes.Buffer
	handler := &Handler{
		Neo4j:   contextCoverageGraph(),
		Content: &contextCoverageProbeStore{narrowErr: err},
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/context", nil).WithContext(ctx)
	req.SetPathValue("repo_id", "repo-1")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	span.End()

	if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("body leaked backend error text: %s", rec.Body.String())
	}
	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	return rec, ended[0], logs.String()
}

// TestRepositoryContextCoverageClientCancelAnswers499 is the #7674 F2
// regression: a client cancel during the context route's content coverage
// read was folded into the 200 coverage degrade. It answers 499 with the
// fixed message, an Unset span status, and only the client-cancel event.
func TestRepositoryContextCoverageClientCancelAnswers499(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("private %s: %w", routeFailureCanary, context.Canceled)
	rec, span, _ := serveContextCoverageFailure(t, err, true)
	if rec.Code != 499 {
		t.Fatalf("status = %d, want 499; body = %s", rec.Code, rec.Body.String())
	}
	if got := repoFailureDetail(t, rec.Body.String()); got != repositoryContextCoverageFailedMessage {
		t.Fatalf("detail = %q, want %q", got, repositoryContextCoverageFailedMessage)
	}
	if strings.Contains(rec.Body.String(), "partial_reasons") {
		t.Fatalf("499 body carries partial_reasons: %s", rec.Body.String())
	}
	if code := span.Status().Code.String(); code != "Unset" {
		t.Fatalf("span status = %s, want Unset", code)
	}
	canceled := false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			t.Fatal("client cancel recorded an exception event")
		}
		canceled = canceled || event.Name == tracing.ClientCanceledEvent
	}
	if !canceled {
		t.Fatalf("span lacks the %s event", tracing.ClientCanceledEvent)
	}
}

// TestRepositoryContextCoverageReaderFenceDegradesToGraphCounts pins the F2
// ruling: a reader fence on the context route's content coverage read keeps
// the attributed 200 degrade, because the headline file_count falls back to
// a propagating graph count. The answer carries the graph count and names
// the degraded coverage read.
func TestRepositoryContextCoverageReaderFenceDegradesToGraphCounts(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("private %s: %w", routeFailureCanary, db.ErrReaderStale)
	rec, _, logs := serveContextCoverageFailure(t, err, false)
	assertContextCoverageDegraded(t, rec, logs)
}

// TestRepositoryContextCoverageInnerCancelOnLiveRequestDegrades pins the
// client-cancel conjunction: a context.Canceled from an inner context on a
// live request is a coverage fault that degrades, not a 499.
func TestRepositoryContextCoverageInnerCancelOnLiveRequestDegrades(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("private %s: %w", routeFailureCanary, context.Canceled)
	rec, _, logs := serveContextCoverageFailure(t, err, false)
	assertContextCoverageDegraded(t, rec, logs)
}

// assertContextCoverageDegraded checks the attributed 200 degrade: the graph
// file count, the named partial reason, and the stage log's failure class.
func assertContextCoverageDegraded(t *testing.T, rec *httptest.ResponseRecorder, logs string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	if got, ok := body["file_count"].(float64); !ok || int(got) != contextCoverageGraphFileCount {
		t.Fatalf("file_count = %#v, want the graph count %d", body["file_count"], contextCoverageGraphFileCount)
	}
	reasons := testutil.RequireStringAnySlice(t, body, "partial_reasons")
	if !testutil.AnySliceContains(reasons, contextCoverageDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want %q", reasons, contextCoverageDegradedReason)
	}
	if !strings.Contains(logs, `"failure_class":"repository_context_content_coverage_degraded"`) {
		t.Fatalf("stage logs lack the coverage failure class: %s", logs)
	}
}
