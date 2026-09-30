// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// rejectedStatementError stands in for the graph reader's error when the
// backend rejects a caller-authored statement: Error() is the bounded public
// text, and the redacted backend message rides on the StatementRejection seam.
type rejectedStatementError struct{ message string }

func (rejectedStatementError) Error() string                        { return "graph query failed" }
func (e rejectedStatementError) StatementRejection() (string, bool) { return e.message, true }

// TestCypherRoutesAnswer400ForARejectedCallerStatement pins #7253's review F5:
// the read-only Cypher and graph-visualization routes run the caller's own
// statement, so a backend syntax error is the caller's fault. It answers 400
// invalid_argument with the redacted backend message, so an author (or an MCP
// agent) can fix the query; any other graph failure stays a 500 with the
// bounded text and never carries backend text.
func TestCypherRoutesAnswer400ForARejectedCallerStatement(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name    string
		path    string
		handle  func(*CodeHandler, http.ResponseWriter, *http.Request)
		wantCap string
	}{
		{name: "cypher", path: "/api/v0/code/cypher", handle: (*CodeHandler).handleCypherQuery, wantCap: readOnlyCypherCapability},
		{name: "visualize", path: "/api/v0/code/visualize", handle: (*CodeHandler).handleVisualizeQuery, wantCap: visualizationGraphQueryCapability},
	}
	for _, route := range routes {
		for _, tc := range []struct {
			name       string
			err        error
			wantStatus int
			wantBody   string
			wantCode   ErrorCode
		}{
			{name: "rejected statement", err: rejectedStatementError{message: "Invalid input <REDACTED> expected a clause"}, wantStatus: http.StatusBadRequest, wantBody: "Invalid input <REDACTED> expected a clause", wantCode: ErrorCodeInvalidArgument},
			{name: "other failure", err: errors.New("graph query failed"), wantStatus: http.StatusInternalServerError, wantBody: "graph query failed", wantCode: ErrorCodeInternalError},
		} {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, tc.err
				}}}
				req := httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(`{"cypher_query":"MATCH (n) RETURN n"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
				rec := httptest.NewRecorder()

				route.handle(handler, rec, req)

				if rec.Code != tc.wantStatus {
					t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
				}
				body := rec.Body.String()
				if !strings.Contains(body, tc.wantBody) || !strings.Contains(body, `"code":"`+string(tc.wantCode)+`"`) ||
					!strings.Contains(body, route.wantCap) {
					t.Fatalf("body = %s, want message %q, code %q, capability %q", body, tc.wantBody, tc.wantCode, route.wantCap)
				}
			})
		}
	}
}
