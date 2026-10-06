// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// selectorResolveStage is the stage the security-alert selector's exact
// resolution runs under once the catalog has no match.
const selectorResolveStage = "repository_selector_resolve"

// selectorResolveGraph answers both graph selector reads with rows and err.
func selectorResolveGraph(rows []map[string]any, err error) graph.FakeGraphReader {
	return graph.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return rows, err
		},
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, err
		},
	}
}

// selectorResolveHandler builds a handler whose catalog has no match for the
// selector, so resolution falls through to selector.ResolveExact and its
// graph reads.
func selectorResolveHandler(graphReader querycontract.GraphQuery) *Handler {
	return &Handler{
		Neo4j:                   graphReader,
		Content:                 selectorMatchContent{},
		SecurityAlerts:          selectorScopeLookupStore{},
		SecurityAlertAggregates: selectorScopeLookupStore{},
	}
}

// TestRepositorySelectorResolveLookupFailureAnswers500 is the #7626 regression
// for the security-alert selector's exact-resolution fallback. A graph or
// store failure there that is not a fence or graph-availability verdict is a
// server fault: it answers 500 with exactly one stage_failed record for the
// repository_selector_resolve stage, never the 400 that blamed the client.
// Fence verdicts keep 503/504, an unmatched selector keeps 404, an ambiguous
// one keeps 400, and none of those log stage_failed. No log attribute may
// carry the raw selector.
func TestRepositorySelectorResolveLookupFailureAnswers500(t *testing.T) {
	t.Parallel()

	twoRows := []map[string]any{{"id": "repository:r_a"}, {"id": "repository:r_b"}}
	cases := []struct {
		name       string
		graph      querycontract.GraphQuery
		wantStatus int
		wantSite   string
	}{
		{"bare reader unavailable", selectorResolveGraph(nil, fmt.Errorf("read store: %w", db.ErrReaderUnavailable)), http.StatusInternalServerError, "reader_unavailable"},
		{"plain graph driver error", selectorResolveGraph(nil, errors.New("neo4j: connection reset")), http.StatusInternalServerError, "other"},
		{"graph unavailable", selectorResolveGraph(nil, fmt.Errorf("graph: %w", querycontract.ErrGraphUnavailable)), http.StatusServiceUnavailable, ""},
		{"graph deadline", selectorResolveGraph(nil, fmt.Errorf("graph: %w", querycontract.ErrGraphReadDeadline)), http.StatusGatewayTimeout, ""},
		{"reader stale", selectorResolveGraph(nil, fmt.Errorf("read store: %w", db.ErrReaderStale)), http.StatusServiceUnavailable, ""},
		{"not found", selectorResolveGraph(nil, nil), http.StatusNotFound, ""},
		{"ambiguous", selectorResolveGraph(twoRows, nil), http.StatusBadRequest, ""},
	}
	for _, route := range selectorTestRoutes() {
		for _, tc := range cases {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				var logBuf bytes.Buffer
				handler := selectorResolveHandler(tc.graph)
				handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))

				rec := serveSiblingRoute(t, handler, route.target)

				if rec.Code != tc.wantStatus {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
				}
				records := decodeLogRecords(t, &logBuf)
				assertNoRawSelectorLogged(t, records)
				failed := recordsWithEvent(records, "supply_chain_query.stage_failed")
				if tc.wantStatus != http.StatusInternalServerError {
					if len(failed) != 0 {
						t.Fatalf("stage_failed records = %d on a %d, want 0; log=%s", len(failed), tc.wantStatus, logBuf.String())
					}
					return
				}
				body := rec.Body.String()
				if strings.Contains(body, "read store") || strings.Contains(body, "neo4j") {
					t.Fatalf("500 body leaked the backend error text: %s", body)
				}
				if strings.Contains(body, selectorTestSelector) {
					t.Fatalf("500 body carries the raw selector: %s", body)
				}
				if len(failed) != 1 {
					t.Fatalf("stage_failed records = %d, want 1; log=%s", len(failed), logBuf.String())
				}
				record := failed[0]
				if record["operation"] != route.operation || record["stage"] != selectorResolveStage || record["repo_id"] != "" {
					t.Fatalf("stage_failed identity = %#v, want operation=%q stage=%q repo_id=\"\"", record, route.operation, selectorResolveStage)
				}
				if record["level"] != "ERROR" || record["error_site"] != tc.wantSite {
					t.Fatalf("stage_failed level/site = %v/%v, want ERROR/%s", record["level"], record["error_site"], tc.wantSite)
				}
			})
		}
	}
}

// TestRepositorySelectorResolveLookupFailureRecordsSpanError pins the span
// half of failStage on the exact-resolution stage: the 500 sets the route's
// handler span to Error and records the error as an exception event.
func TestRepositorySelectorResolveLookupFailureRecordsSpanError(t *testing.T) {
	// Not parallel: swaps the package-global queryHandlerTracer.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("repository-selector-resolve-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	for _, route := range selectorTestRoutes() {
		t.Run(route.name, func(t *testing.T) {
			spansBefore := len(recorder.Ended())
			handler := selectorResolveHandler(selectorResolveGraph(nil, errors.New("neo4j: connection reset")))
			rec := serveSiblingRoute(t, handler, route.target)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
			}
			var handlerSpan sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended()[spansBefore:] {
				if span.Name() == route.spanName {
					handlerSpan = span
				}
			}
			if handlerSpan == nil {
				t.Fatalf("no ended %s span", route.spanName)
			}
			if handlerSpan.Status().Code != codes.Error {
				t.Fatalf("handler span status = %v, want Error", handlerSpan.Status().Code)
			}
			hasException := false
			for _, event := range handlerSpan.Events() {
				if event.Name == "exception" {
					hasException = true
				}
			}
			if !hasException {
				t.Fatalf("handler span has no recorded exception event; events=%#v", handlerSpan.Events())
			}
		})
	}
}
