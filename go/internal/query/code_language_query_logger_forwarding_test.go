// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCodeHandlerMountForwardsLoggerToLanguageQueryHandler is the #5761 P1-1
// review-fix regression: in production, LanguageQueryHandler is never
// constructed directly. It only ever exists as a value built inside
// CodeHandler.Mount (code.go) and fed by the two wiring routers
// (cmd/api/wiring_router.go, cmd/mcp-server/wiring_router.go). The
// pre-existing TestHandleLanguageQueryGenericFailureStaysStaticAndLogsFailureClass
// (language_query_generic_failure_test.go) proves the log against a
// hand-built *LanguageQueryHandler{Logger: logger, ...}, which never
// exercises the `Logger: h.Logger` pass-through line CodeHandler.Mount
// actually contains -- deleting that line left every package in
// `./internal/query ./internal/mcp ./cmd/api ./cmd/mcp-server` green.
//
// This test instead builds only a *CodeHandler (never a
// *LanguageQueryHandler directly), calls the real CodeHandler.Mount, and
// drives the request through the resulting mux, so it exercises the exact
// construction path production traffic takes. Because the 500 response body
// is deliberately static with no cause in the envelope, this log is the sole
// operator signal for a generic language-query failure -- if
// `Logger: h.Logger` is ever deleted from CodeHandler.Mount, the log buffer
// stays empty and this test fails.
func TestCodeHandlerMountForwardsLoggerToLanguageQueryHandler(t *testing.T) {
	t.Parallel()

	genericErr := errors.New("private driver detail")
	var logBuf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, genericErr
			},
		},
		Logger: logger,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"function","query":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(logBuf.String())), &record); err != nil {
		t.Fatalf("json.Unmarshal(log record) error = %v, log = %q", err, logBuf.String())
	}
	if got, want := record["msg"], "language query failed"; got != want {
		t.Fatalf("log msg = %#v, want %q", got, want)
	}
	if got, want := record["failure_class"], "language_query.graph_backed"; got != want {
		t.Fatalf("log failure_class = %#v, want %q", got, want)
	}
}
