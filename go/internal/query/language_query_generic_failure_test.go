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

// TestHandleLanguageQueryGenericFailureStaysStaticAndLogsFailureClass is the
// #5761 P1-2 and P1-3 review-fix regression, combined because both gaps share
// the same drive-a-generic-error-through-each-branch table.
//
// P1-2: graphReadSweepCases() (graph_read_error_sweep_shared_test.go) only
// ever drives ErrGraphUnavailable/ErrGraphReadDeadline through this route,
// both of which WriteGraphReadError recognizes and answers BEFORE the
// generic `WriteError(w, http.StatusInternalServerError, "language query
// failed")` branch is ever reached. No existing test therefore proves that
// branch's body is the static string rather than err.Error() -- replacing
// all four occurrences with err.Error() (reintroducing the exact
// driver-text leak #5761 exists to fix) left `go test ./internal/query`
// green. This test drives a plain, non-bounded error through each of the
// four dispatch branches and asserts the response body is the static
// message and never contains the private error text.
//
// P1-3: logQueryFailure (language_queries.go) and its four
// "language_query.*" failure_class values had no test referencing them at
// all -- early-returning the whole logger body, or renaming any of the four
// constants to "WRONG", left the package green. This test captures a slog
// buffer per case and asserts the emitted record's failure_class equals the
// branch-specific constant.
func TestHandleLanguageQueryGenericFailureStaysStaticAndLogsFailureClass(t *testing.T) {
	t.Parallel()

	genericErr := errors.New("private driver detail")

	cases := []struct {
		name             string
		body             string
		wantFailureClass string
		wantEntityType   string
		buildHandler     func(logger *slog.Logger) *LanguageQueryHandler
	}{
		{
			name:             "guard",
			body:             `{"language":"go","entity_type":"guard","query":"x"}`,
			wantFailureClass: "language_query.guard",
			wantEntityType:   "guard",
			buildHandler: func(logger *slog.Logger) *LanguageQueryHandler {
				return &LanguageQueryHandler{
					Logger: logger,
					Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
						return nil, genericErr
					}},
				}
			},
		},
		{
			name:             "graph_backed",
			body:             `{"language":"go","entity_type":"function","query":"x"}`,
			wantFailureClass: "language_query.graph_backed",
			wantEntityType:   "function",
			buildHandler: func(logger *slog.Logger) *LanguageQueryHandler {
				return &LanguageQueryHandler{
					Logger: logger,
					Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
						return nil, genericErr
					}},
				}
			},
		},
		{
			name:             "graph_first_content_backed",
			body:             `{"language":"go","entity_type":"sql_table","query":"x"}`,
			wantFailureClass: "language_query.graph_first_content_backed",
			wantEntityType:   "sql_table",
			buildHandler: func(logger *slog.Logger) *LanguageQueryHandler {
				return &LanguageQueryHandler{
					Logger: logger,
					Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
						return nil, genericErr
					}},
				}
			},
		},
		{
			name:             "content_backed",
			body:             `{"language":"go","entity_type":"variable","query":"x"}`,
			wantFailureClass: "language_query.content_backed",
			wantEntityType:   "variable",
			buildHandler: func(logger *slog.Logger) *LanguageQueryHandler {
				return &LanguageQueryHandler{
					Logger:  logger,
					Content: fakeErrLanguageQueryContentStore{err: genericErr},
				}
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var logBuf strings.Builder
			logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
			handler := test.buildHandler(logger)

			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleLanguageQuery(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
			}
			if want := `"detail":"language query failed"`; !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("body = %s, want it to contain %s", rec.Body.String(), want)
			}
			if strings.Contains(rec.Body.String(), "private") {
				t.Fatalf("body leaked private driver text: %s", rec.Body.String())
			}

			var record map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(logBuf.String())), &record); err != nil {
				t.Fatalf("json.Unmarshal(log record) error = %v, log = %s", err, logBuf.String())
			}
			if got := record["msg"]; got != "language query failed" {
				t.Fatalf("log msg = %#v, want %q", got, "language query failed")
			}
			if got := record["failure_class"]; got != test.wantFailureClass {
				t.Fatalf("log failure_class = %#v, want %q", got, test.wantFailureClass)
			}
			if got := record["language"]; got != "go" {
				t.Fatalf("log language = %#v, want %q", got, "go")
			}
			if got := record["entity_type"]; got != test.wantEntityType {
				t.Fatalf("log entity_type = %#v, want %q", got, test.wantEntityType)
			}
		})
	}
}
