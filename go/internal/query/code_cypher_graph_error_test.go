// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCypherQueryMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   ErrorCode
	}{
		{name: "unavailable", err: fmt.Errorf("private address: %w", ErrGraphUnavailable), wantStatus: http.StatusServiceUnavailable, wantCode: ErrorCodeBackendUnavailable},
		{name: "deadline", err: fmt.Errorf("private query: %w", ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, wantCode: ErrorCodeBackendTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/cypher", strings.NewReader(`{"cypher_query":"RETURN 1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleCypherQuery(rec, req)

			if rec.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, test.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+string(test.wantCode)+`"`) {
				t.Fatalf("body = %s, want code %q", rec.Body.String(), test.wantCode)
			}
			if strings.Contains(rec.Body.String(), "private") {
				t.Fatalf("body leaked private cause: %s", rec.Body.String())
			}
		})
	}
}

func TestHandleVisualizeQueryMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   ErrorCode
	}{
		{name: "unavailable", err: fmt.Errorf("private address: %w", ErrGraphUnavailable), wantStatus: http.StatusServiceUnavailable, wantCode: ErrorCodeBackendUnavailable},
		{name: "deadline", err: fmt.Errorf("private query: %w", ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, wantCode: ErrorCodeBackendTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/visualize", strings.NewReader(`{"cypher_query":"RETURN 1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleVisualizeQuery(rec, req)

			if rec.Code != test.wantStatus || !strings.Contains(rec.Body.String(), `"code":"`+string(test.wantCode)+`"`) {
				t.Fatalf("response = status %d body=%s, want status %d code %q", rec.Code, rec.Body.String(), test.wantStatus, test.wantCode)
			}
			if strings.Contains(rec.Body.String(), "private") {
				t.Fatalf("body leaked private cause: %s", rec.Body.String())
			}
		})
	}
}
