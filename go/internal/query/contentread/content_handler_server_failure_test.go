// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// failingContentReadStore fails every content read and search the five
// content routes issue after selector resolution with err.
type failingContentReadStore struct {
	content.FakePortContentStore
	err error
}

func (s failingContentReadStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) GetFileLines(context.Context, string, string, int, int) (*querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) SearchFileContent(context.Context, string, string, int) ([]querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) SearchFileContentAnyRepo(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) SearchEntityContent(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) SearchEntityContentAnyRepo(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return nil, s.err
}

// TestContentRoutesServerFailureAnswersFixedMessage is the #7626 regression
// for the content reads that run after the selector resolved. A failed read
// answered 500 with err.Error() as the body, which carries backend text, and
// left the span untouched. It must answer the route's fixed message with a
// span error, a reader fence must answer the retryable 503, and a client
// cancel must answer 499 without a span error.
func TestContentRoutesServerFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name    string
		path    string
		body    string
		message string
	}{
		{"file read", "/api/v0/content/files/read", `{"repo_id":"repository:r_ok","relative_path":"src/app.go"}`, contentFileReadFailedMessage},
		{"file lines", "/api/v0/content/files/lines", `{"repo_id":"repository:r_ok","relative_path":"src/app.go","start_line":1,"end_line":2}`, contentFileReadFailedMessage},
		{"entity read", "/api/v0/content/entities/read", `{"entity_id":"content-entity:e_1"}`, contentEntityReadFailedMessage},
		{"file search repo", "/api/v0/content/files/search", `{"repo_id":"repository:r_ok","query":"handler"}`, contentFileSearchFailedMessage},
		{"file search any repo", "/api/v0/content/files/search", `{"query":"handler"}`, contentFileSearchFailedMessage},
		{"entity search repo", "/api/v0/content/entities/search", `{"repo_id":"repository:r_ok","query":"handler"}`, contentEntitySearchFailedMessage},
		{"entity search any repo", "/api/v0/content/entities/search", `{"query":"handler"}`, contentEntitySearchFailedMessage},
	}
	cases := []struct {
		name          string
		err           error
		cancel        bool
		wantStatus    int
		wantSpanError bool
	}{
		{"backend failure", errors.New(`private pq: relation "content_files" does not exist`), false, http.StatusInternalServerError, true},
		{"reader stale", fmt.Errorf("private read store: %w", db.ErrReaderStale), false, http.StatusServiceUnavailable, false},
		{"client cancel", fmt.Errorf("private read store: %w", context.Canceled), true, querycontract.StatusClientClosedRequest, false},
	}
	for _, route := range routes {
		for _, tc := range cases {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				recorder := tracetest.NewSpanRecorder()
				provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
				t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
				ctx, span := provider.Tracer("contentread-server-failure-test").Start(context.Background(), "http")
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				if tc.cancel {
					cancel()
				}

				mux := http.NewServeMux()
				(&ContentHandler{Content: failingContentReadStore{err: tc.err}}).Mount(mux)
				req := httptest.NewRequest(http.MethodPost, route.path, bytes.NewBufferString(route.body)).WithContext(ctx)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				span.End()

				if rec.Code != tc.wantStatus {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
				}
				body := rec.Body.String()
				if strings.Contains(body, "private") {
					t.Fatalf("body leaked the backend error text: %s", body)
				}
				if tc.wantStatus == http.StatusServiceUnavailable && rec.Header().Get("Retry-After") == "" {
					t.Fatal("reader fence 503 is missing Retry-After")
				}
				if tc.wantStatus != http.StatusServiceUnavailable && !strings.Contains(body, route.message) {
					t.Fatalf("body = %s, want the fixed %q", body, route.message)
				}
				assertContentServerFailureSpan(t, recorder.Ended(), tc.wantSpanError, tc.cancel, route.message)
			})
		}
	}
}

func assertContentServerFailureSpan(t *testing.T, ended []sdktrace.ReadOnlySpan, wantError, wantCanceled bool, message string) {
	t.Helper()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	span := ended[0]
	hasException, hasCanceled := false, false
	for _, event := range span.Events() {
		hasException = hasException || event.Name == "exception"
		hasCanceled = hasCanceled || event.Name == tracing.ClientCanceledEvent
	}
	status := span.Status()
	if (status.Code == codes.Error) != wantError || hasException != wantError {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			status.Code, status.Description, hasException, wantError)
	}
	if wantError && status.Description != message {
		t.Fatalf("span status description = %q, want the fixed %q", status.Description, message)
	}
	if hasCanceled != wantCanceled {
		t.Fatalf("%s event recorded = %v, want %v", tracing.ClientCanceledEvent, hasCanceled, wantCanceled)
	}
}
