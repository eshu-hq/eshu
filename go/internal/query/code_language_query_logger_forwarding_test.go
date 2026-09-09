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

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestCodeHandlerMountForwardsLoggerToLanguageQueryHandler is the #5761 P1-1
// review-fix regression, relocated for the #6060 lane-A move. LanguageQueryHandler
// used to be built inside CodeHandler.Mount, and this test pinned the
// `Logger: h.Logger` pass-through there. The mount has since been hoisted:
// CodeHandler lives in internal/query/codequery, which cannot import package
// query back to build a LanguageQueryHandler, so APIRouter carries a sibling
// Language field that both cmd wirings construct with Neo4j/Content/Profile/
// Logger (cmd/api/wiring_router.go, cmd/mcp-server/wiring_router.go).
//
// This test pins the relocated contract at the router level: it builds the
// APIRouter the way production does -- Code and Language sharing one logger --
// mounts it, and drives a language-query whose graph read fails generically.
// Because the 500 response body is deliberately static with no cause in the
// envelope, this log is the sole operator signal for a generic language-query
// failure: if Language is ever wired without its Logger, the log buffer stays
// empty and this test fails.
func TestCodeHandlerMountForwardsLoggerToLanguageQueryHandler(t *testing.T) {
	t.Parallel()

	genericErr := errors.New("private driver detail")
	var logBuf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	graph := querytestutil.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, genericErr
		},
	}

	router := &APIRouter{
		Code:     &CodeHandler{Neo4j: graph, Logger: logger},
		Language: &LanguageQueryHandler{Neo4j: graph, Logger: logger},
	}
	mux := http.NewServeMux()
	router.Mount(mux)

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
