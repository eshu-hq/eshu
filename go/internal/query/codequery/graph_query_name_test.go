// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestHandleComplexityAndInspectSetGraphQueryName pins the #7006 telemetry
// fix: both handlers must thread a bounded query name through
// querycontract.WithGraphQueryName before reaching the graph reader, so
// query.graph_read.warning can name which query hit the deadline.
func TestHandleComplexityAndInspectSetGraphQueryName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		path     string
		body     string
		wantName string
	}{
		{"complexity_list", "/api/v0/code/complexity", `{}`, "code_quality.complexity"},
		{"quality_inspect", "/api/v0/code/quality/inspect", `{"check":"complexity"}`, "code_quality.refactoring"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotName string
			handler := &CodeHandler{
				Profile: ProfileLocalAuthoritative,
				Neo4j: fakeGraphReader{
					run: func(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
						gotName = querycontract.GraphQueryNameFromContext(ctx)
						return nil, nil
					},
					runSingle: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
						gotName = querycontract.GraphQueryNameFromContext(ctx)
						return nil, nil
					},
				},
			}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if gotName != tc.wantName {
				t.Fatalf("graph query name = %q, want %q", gotName, tc.wantName)
			}
		})
	}
}
