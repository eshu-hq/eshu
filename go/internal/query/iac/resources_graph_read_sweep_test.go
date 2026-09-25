// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// This file is this package's own copy of root package query's
// graph_read_error_sweep_shared_test.go, graph_reader_test_adapter_test.go,
// and the TestIaCListResourcesGraphReadSweep case from
// graph_read_error_aggregate_test.go (#6642 Part A). The sweep case moved
// here with its subject (listResources); the two small shared fixtures
// (graphReadSweepCase table and the fakeGraphReader adapter) are duplicated
// rather than exported from root, matching graph_reader_test_adapter_test.go's
// own documented precedent for codequery's identically-shaped copy.

// fakeGraphReader adapts graph.FakeGraphReader the same way root's
// copy and codequery's copy do.
type fakeGraphReader struct {
	run       func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle func(context.Context, string, map[string]any) (map[string]any, error)
}

func (f fakeGraphReader) delegate() graph.FakeGraphReader {
	return graph.FakeGraphReader{
		RunFn:       f.run,
		RunSingleFn: f.runSingle,
	}
}

func (f fakeGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return f.delegate().Run(ctx, cypher, params)
}

func (f fakeGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return f.delegate().RunSingle(ctx, cypher, params)
}

// graphReadSweepCase is one bounded graph-read availability case: a sentinel
// wrapped in a private detail that must map to the stable HTTP contract
// without leaking the wrapped cause.
type graphReadSweepCase struct {
	name       string
	err        error
	wantStatus int
	wantCode   querycontract.ErrorCode
}

// graphReadSweepCases is the shared unavailable/deadline table, duplicated
// from root's identically named function.
func graphReadSweepCases() []graphReadSweepCase {
	return []graphReadSweepCase{
		{name: "unavailable", err: fmt.Errorf("private graph detail: %w", querycontract.ErrGraphUnavailable), wantStatus: http.StatusServiceUnavailable, wantCode: querycontract.ErrorCodeBackendUnavailable},
		{name: "deadline", err: fmt.Errorf("private graph detail: %w", querycontract.ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, wantCode: querycontract.ErrorCodeBackendTimeout},
	}
}

// assertGraphReadSweepResponse asserts the bounded-availability status, the
// envelope error code, and that the private wrapped cause never reaches the
// response body.
func assertGraphReadSweepResponse(t *testing.T, rec *httptest.ResponseRecorder, test graphReadSweepCase) {
	t.Helper()
	if rec.Code != test.wantStatus {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, test.wantStatus, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"`+string(test.wantCode)+`"`) {
		t.Fatalf("body = %s, want code %q", rec.Body.String(), test.wantCode)
	}
	if strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("body leaked private cause: %s", rec.Body.String())
	}
}

// TestIaCListResourcesGraphReadSweep drives listResources through a graph
// error, with one candidate already resolved by the inventory store so the
// handler actually reaches h.Graph.Run, to confirm the graph
// unavailable/deadline sentinels map to 503/504 instead of a bare 500. Moved
// from root's graph_read_error_aggregate_test.go (#6642 Part A) because it is
// this package's only case in that shared file; the other cases there test
// unrelated handler families and stay in root.
func TestIaCListResourcesGraphReadSweep(t *testing.T) {
	t.Parallel()

	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			inventory := &stubIaCInventoryStore{candidates: []InventoryCandidate{
				{ID: "a1", Name: "aws_s3_bucket.logs", GenerationID: "generation-active"},
			}}
			graph := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}
			handler := &Handler{Graph: graph, Inventory: inventory}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=5", nil)
			req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
