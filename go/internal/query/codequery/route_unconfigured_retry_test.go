// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// routeToCallerRequest posts a valid route-to-caller body through the real
// handler mux.
func routeToCallerMuxRequest(t *testing.T, handler *CodeHandler) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/routes/callers", strings.NewReader(`{"path":"/foo","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestRouteToCallerWithoutGraphBackendCarriesNoRetryAfter pins the #7523 review
// fix: an unconfigured graph backend is a permanent profile/config state, so
// its 503 backend_unavailable must not tell the client to retry.
func TestRouteToCallerWithoutGraphBackendCarriesNoRetryAfter(t *testing.T) {
	t.Parallel()
	rec := routeToCallerMuxRequest(t, &CodeHandler{Profile: ProfileLocalAuthoritative})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"backend_unavailable"`) {
		t.Fatalf("body = %s, want backend_unavailable", rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q on a permanent unconfigured-backend 503, want none", got)
	}
}

// TestRouteToCallerGraphUnavailableKeepsRetryAfter keeps the transient
// graph-unavailable verdict, produced by the shared mapping, retryable through
// the same mux.
func TestRouteToCallerGraphUnavailableKeepsRetryAfter(t *testing.T) {
	t.Parallel()
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, querycontract.ErrGraphUnavailable
		}},
	}
	rec := routeToCallerMuxRequest(t, handler)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Fatalf("Retry-After missing on the transient graph-unavailable 503")
	}
}
