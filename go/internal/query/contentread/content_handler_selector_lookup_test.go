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

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// contentLookupSelector is non-canonical, so resolution reaches the catalog
// read instead of short-circuiting on the canonical-id shape.
const contentLookupSelector = "payments-svc"

// contentLookupStore fails the selector's catalog read with matchErr, or
// answers from the embedded fixture catalog when matchErr is nil.
type contentLookupStore struct {
	content.FakePortContentStore
	matchErr error
}

func (s contentLookupStore) MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error) {
	if s.matchErr != nil {
		return nil, s.matchErr
	}
	return s.FakePortContentStore.MatchRepositories(ctx, selector)
}

// TestContentSelectorLookupFailureMapping is the #7626 regression for the
// content read and search routes. Their selector errors had no fence mapping,
// so a stale reader answered 400, and a plain store failure answered 400 with
// the backend text in the body. A fence verdict now answers 503, a lookup
// failure answers 500 with the fixed body and an error on the request span, and
// an unmatched or ambiguous selector keeps 404 or 400 without touching the span.
func TestContentSelectorLookupFailureMapping(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name string
		path string
		body string
	}{
		{"file read", "/api/v0/content/files/read", `{"repo_id":%q,"relative_path":"src/app.go"}`},
		{"file lines", "/api/v0/content/files/lines", `{"repo_id":%q,"relative_path":"src/app.go","start_line":1,"end_line":2}`},
		{"file search repo_id", "/api/v0/content/files/search", `{"repo_id":%q,"query":"handler"}`},
		{"file search repo_ids", "/api/v0/content/files/search", `{"repo_ids":["repository:r_ok",%q],"query":"handler"}`},
		{"entity search repo_id", "/api/v0/content/entities/search", `{"repo_id":%q,"query":"handler"}`},
		{"entity search repo_ids", "/api/v0/content/entities/search", `{"repo_ids":[%q],"query":"handler"}`},
	}
	ambiguous := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: contentLookupSelector},
		{ID: "repository:r_b", Name: contentLookupSelector},
	}}
	cases := []struct {
		name          string
		store         querycontract.ContentStore
		wantStatus    int
		wantSpanError bool
	}{
		{"plain store error", contentLookupStore{matchErr: errors.New(`private pq: relation "repositories" does not exist`)}, http.StatusInternalServerError, true},
		{"bare reader unavailable", contentLookupStore{matchErr: fmt.Errorf("private read store: %w", db.ErrReaderUnavailable)}, http.StatusInternalServerError, true},
		{"reader stale", contentLookupStore{matchErr: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, http.StatusServiceUnavailable, false},
		{"reader pool wait timeout", contentLookupStore{matchErr: fmt.Errorf("private: %w", errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded))}, http.StatusServiceUnavailable, false},
		{"not found", contentLookupStore{}, http.StatusNotFound, false},
		{"ambiguous", ambiguous, http.StatusBadRequest, false},
	}
	for _, route := range routes {
		for _, tc := range cases {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				recorder := tracetest.NewSpanRecorder()
				provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
				t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
				ctx, span := provider.Tracer("contentread-selector-test").Start(context.Background(), "http")

				mux := http.NewServeMux()
				(&ContentHandler{Content: tc.store}).Mount(mux)
				req := httptest.NewRequest(http.MethodPost, route.path,
					bytes.NewBufferString(fmt.Sprintf(route.body, contentLookupSelector))).WithContext(ctx)
				req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
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
				assertContentLookupSpan(t, recorder.Ended(), tc.wantSpanError)
				if tc.wantStatus != http.StatusInternalServerError {
					return
				}
				if !strings.Contains(body, selector.LookupFailureMessage) {
					t.Fatalf("body = %s, want the fixed %q message", body, selector.LookupFailureMessage)
				}
				if strings.Contains(body, contentLookupSelector) {
					t.Fatalf("500 body carries the raw selector: %s", body)
				}
			})
		}
	}
}

func assertContentLookupSpan(t *testing.T, ended []sdktrace.ReadOnlySpan, want bool) {
	t.Helper()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	span := ended[0]
	hasException := false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			hasException = true
		}
	}
	if isError := span.Status().Code == codes.Error; isError != want || hasException != want {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			span.Status().Code, span.Status().Description, hasException, want)
	}
	if strings.Contains(span.Status().Description, contentLookupSelector) {
		t.Fatalf("span status description %q carries the raw selector", span.Status().Description)
	}
}
