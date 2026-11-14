// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// requestTestSelector is the raw, non-canonical selector the request tests
// send, so resolution reaches the catalog and graph reads instead of
// short-circuiting on LooksCanonicalRepositoryID.
const requestTestSelector = "payments-svc"

// failingCatalog fails the catalog read with err.
type failingCatalog struct {
	content.FakePortContentStore
	err error
}

func (f failingCatalog) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, f.err
}

func graphRunFailing(err error) graph.FakeGraphReader {
	return graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		return nil, err
	}}
}

// graphFallbackFailing answers the ordered read with no rows and fails the
// RunSingle fallback read, the second of the two graph lookups.
func graphFallbackFailing(err error) graph.FakeGraphReader {
	return graph.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, nil
		},
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, err
		},
	}
}

// TestResolveForRequestWithAccessMapsLookupFailureTo500 is the #7626
// regression for the shared request helper. A store or graph failure that is
// not a reader fence or graph-availability verdict is a server fault, so it
// answers 500 with a fixed body and records the error on the request span,
// instead of the 400 that told the client its request was malformed. Fence
// verdicts keep 503/504, an unmatched selector keeps 404, an ambiguous one
// answers the structured 409 (#10655), and none of those touch the span.
func TestResolveForRequestWithAccessMapsLookupFailureTo500(t *testing.T) {
	t.Parallel()

	ambiguous := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: requestTestSelector},
		{ID: "repository:r_b", Name: requestTestSelector},
	}}
	cases := []struct {
		name          string
		graph         querycontract.GraphQuery
		content       querycontract.ContentStore
		wantStatus    int
		wantSpanError bool
	}{
		{"store bare reader unavailable", nil, failingCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderUnavailable)}, http.StatusInternalServerError, true},
		{"store plain sql error", nil, failingCatalog{err: errors.New(`private pq: relation "repositories" does not exist`)}, http.StatusInternalServerError, true},
		{"graph plain driver error on ordered read", graphRunFailing(errors.New("private neo4j: connection reset")), nil, http.StatusInternalServerError, true},
		{"graph plain driver error on fallback read", graphFallbackFailing(errors.New("private neo4j: connection reset")), nil, http.StatusInternalServerError, true},
		{"store reader stale", nil, failingCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, http.StatusServiceUnavailable, false},
		{"graph unavailable", graphRunFailing(fmt.Errorf("private: %w", querycontract.ErrGraphUnavailable)), nil, http.StatusServiceUnavailable, false},
		{"graph deadline", graphRunFailing(fmt.Errorf("private: %w", querycontract.ErrGraphReadDeadline)), nil, http.StatusGatewayTimeout, false},
		{"not found", graphRunFailing(nil), nil, http.StatusNotFound, false},
		{"ambiguous", nil, ambiguous, http.StatusConflict, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ctx, span := provider.Tracer("selector-request-test").Start(context.Background(), "handler")

			req := httptest.NewRequest(http.MethodGet, "/route?repository_id="+requestTestSelector, nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			repoID, ok := ResolveForRequestWithAccess(rec, req, tc.graph, tc.content, requestTestSelector,
				querycontract.RepositoryAccessFilter{AllScopes: true}, "code_search.fuzzy_symbol")
			span.End()

			if ok || repoID != "" {
				t.Fatalf("ResolveForRequestWithAccess() = (%q, %v), want (\"\", false)", repoID, ok)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			ended := recorder.Ended()
			if len(ended) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(ended))
			}
			assertRequestSpanError(t, ended[0], tc.wantSpanError)
			if tc.wantStatus != http.StatusInternalServerError {
				return
			}
			body := rec.Body.String()
			if !strings.Contains(body, LookupFailureMessage) {
				t.Fatalf("body = %s, want the fixed %q message", body, LookupFailureMessage)
			}
			if strings.Contains(body, requestTestSelector) || strings.Contains(body, "private") {
				t.Fatalf("body = %s leaks the raw selector or the backend error text", body)
			}
		})
	}
}

// TestWriteLookupFailureAnswersOnlyLookupFailures pins the helper every
// selector caller outside ResolveForRequestWithAccess uses (#7626): a
// LookupError, bare or wrapped, answers 500 with the fixed body and records the
// error on the request span; every other error writes nothing, touches no span,
// and reports false so the caller's own 404/400 mapping runs.
func TestWriteLookupFailureAnswersOnlyLookupFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		err   error
		wantW bool
	}{
		{"lookup error", LookupError{Err: errors.New("private neo4j: connection reset")}, true},
		{"wrapped lookup error", fmt.Errorf("resolve: %w", LookupError{Err: errors.New("private pq: boom")}), true},
		{"not found", NotFoundError{Selector: requestTestSelector}, false},
		{"ambiguous", AmbiguousError{Selector: requestTestSelector, Matches: []string{"a", "b"}}, false},
		{"plain error", errors.New("private other"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ctx, span := provider.Tracer("selector-request-test").Start(context.Background(), "handler")

			req := httptest.NewRequest(http.MethodGet, "/route", nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			wrote := WriteLookupFailure(rec, req, tc.err)
			span.End()

			if wrote != tc.wantW {
				t.Fatalf("WriteLookupFailure() = %v, want %v", wrote, tc.wantW)
			}
			assertRequestSpanError(t, recorder.Ended()[0], tc.wantW)
			body := rec.Body.String()
			if !tc.wantW {
				if body != "" {
					t.Fatalf("body = %q, want nothing written", body)
				}
				return
			}
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			if !strings.Contains(body, LookupFailureMessage) || strings.Contains(body, "private") {
				t.Fatalf("body = %s, want the fixed %q message and no backend text", body, LookupFailureMessage)
			}
		})
	}
}

// TestWriteLookupFailureClientCancelAnswers499 proves a lookup that failed
// because the caller canceled its own request is not a server fault: it
// answers 499 with the fixed body, leaves the span status unset, records no
// exception, and adds only the client-cancel event.
func TestWriteLookupFailureClientCancelAnswers499(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("selector-request-test").Start(context.Background(), "handler")
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/route", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	err := LookupError{Err: fmt.Errorf("private match repositories: %w", context.Canceled)}
	if !WriteLookupFailure(rec, req, err) {
		t.Fatal("WriteLookupFailure() = false, want true for a LookupError")
	}
	span.End()

	if rec.Code != querycontract.StatusClientClosedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, querycontract.StatusClientClosedRequest)
	}
	if body := rec.Body.String(); !strings.Contains(body, LookupFailureMessage) || strings.Contains(body, "private") {
		t.Fatalf("body = %s, want the fixed %q message and no backend text", body, LookupFailureMessage)
	}
	ended := recorder.Ended()[0]
	if ended.Status().Code != codes.Unset {
		t.Fatalf("span status = %v (%q), want Unset for a client cancel", ended.Status().Code, ended.Status().Description)
	}
	canceled := false
	for _, event := range ended.Events() {
		if event.Name == "exception" {
			t.Fatal("exception event recorded for a client cancel")
		}
		if event.Name == tracing.ClientCanceledEvent {
			canceled = true
		}
	}
	if !canceled {
		t.Fatalf("%s event missing", tracing.ClientCanceledEvent)
	}
}

// TestResolveForRequestWithAccessAnswersAmbiguityWithStructuredCandidates is
// the #10655 regression. Two repositories sharing one name must answer 409
// with a stable ambiguous code and a candidates array of objects, not a bare
// 400 whose ids live only inside a sentence.
func TestResolveForRequestWithAccessAnswersAmbiguityWithStructuredCandidates(t *testing.T) {
	t.Parallel()

	store := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: requestTestSelector},
		{ID: "repository:r_b", Name: requestTestSelector},
	}}

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("selector-request-test").Start(context.Background(), "handler")

	req := httptest.NewRequest(http.MethodGet, "/route?repository_id="+requestTestSelector, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	repoID, ok := ResolveForRequestWithAccess(rec, req, nil, store, requestTestSelector,
		querycontract.RepositoryAccessFilter{AllScopes: true}, "code_search.fuzzy_symbol")
	span.End()

	if ok || repoID != "" {
		t.Fatalf("ResolveForRequestWithAccess() = (%q, %v), want (\"\", false)", repoID, ok)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var body struct {
		Status     string `json:"status"`
		Code       string `json:"code"`
		Selector   string `json:"selector"`
		Candidates []struct {
			RepoID string `json:"repo_id"`
		} `json:"candidates"`
		Truncated bool   `json:"truncated"`
		Detail    string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body = %s (decode err %v), want a structured ambiguity body", rec.Body.String(), err)
	}
	if body.Status != "ambiguous" || body.Code != string(querycontract.ErrorCodeAmbiguous) {
		t.Fatalf("body = %s, want status ambiguous with the stable ambiguous code", rec.Body.String())
	}
	if body.Selector != requestTestSelector {
		t.Fatalf("body = %s, want selector %q", rec.Body.String(), requestTestSelector)
	}
	if len(body.Candidates) != 2 ||
		body.Candidates[0].RepoID != "repository:r_a" ||
		body.Candidates[1].RepoID != "repository:r_b" {
		t.Fatalf("body = %s, want both colliding ids as candidate objects in order", rec.Body.String())
	}
	if body.Truncated {
		t.Fatalf("body = %s, want truncated false: resolution returns every match", rec.Body.String())
	}
	if !strings.Contains(body.Detail, requestTestSelector) {
		t.Fatalf("body = %s, want the detail to name the selector", rec.Body.String())
	}
	assertRequestSpanError(t, recorder.Ended()[0], false)
}

// TestWriteAmbiguousErrorNormalizesMatches pins criterion 2 of #10655: an
// AmbiguousError built with an empty or duplicated Matches list still answers
// a well-formed 409 without a panic or a duplicate candidate.
func TestWriteAmbiguousErrorNormalizesMatches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		matches    []string
		wantIDs    []string
		wantDetail string
	}{
		{"empty matches", nil, []string{}, `repository selector "payments-svc" matched multiple repositories: `},
		{"duplicate ids", []string{"repository:r_b", "repository:r_a", "repository:r_b", ""}, []string{"repository:r_a", "repository:r_b"}, "matched multiple repositories"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/route", nil)
			rec := httptest.NewRecorder()
			if !WriteAmbiguousError(rec, req,
				AmbiguousError{Selector: requestTestSelector, Matches: tc.matches},
				"code_search.fuzzy_symbol") {
				t.Fatal("WriteAmbiguousError() = false, want true for an AmbiguousError")
			}
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
			}
			var body struct {
				Candidates []AmbiguousCandidate `json:"candidates"`
				Detail     string               `json:"detail"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body = %s (decode err %v), want a structured ambiguity body", rec.Body.String(), err)
			}
			gotIDs := make([]string, 0, len(body.Candidates))
			for _, candidate := range body.Candidates {
				gotIDs = append(gotIDs, candidate.RepoID)
			}
			if fmt.Sprintf("%q", gotIDs) != fmt.Sprintf("%q", tc.wantIDs) {
				t.Fatalf("candidates = %q, want %q; body = %s", gotIDs, tc.wantIDs, rec.Body.String())
			}
			if !strings.Contains(body.Detail, tc.wantDetail) {
				t.Fatalf("body = %s, want detail containing %q", rec.Body.String(), tc.wantDetail)
			}
		})
	}

	t.Run("non-ambiguous writes nothing", func(t *testing.T) {
		t.Parallel()

		for _, err := range []error{
			NotFoundError{Selector: requestTestSelector},
			errors.New("private other"),
			nil,
		} {
			req := httptest.NewRequest(http.MethodGet, "/route", nil)
			rec := httptest.NewRecorder()
			if WriteAmbiguousError(rec, req, err, "code_search.fuzzy_symbol") {
				t.Fatalf("WriteAmbiguousError(%v) = true, want false", err)
			}
			if rec.Body.String() != "" {
				t.Fatalf("body = %q, want nothing written for %v", rec.Body.String(), err)
			}
		}
	})
}

// TestResolveAmbiguityBodyHoldsOnlyReadableIDs pins criterion 3 of #10655: a
// grant-scoped caller sees only readable ids in the ambiguity body. A third
// repository sharing the name but outside the grant never appears.
func TestResolveAmbiguityBodyHoldsOnlyReadableIDs(t *testing.T) {
	t.Parallel()

	store := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: requestTestSelector},
		{ID: "repository:r_b", Name: requestTestSelector},
		{ID: "repository:hidden", Name: requestTestSelector},
	}}
	access := querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:r_a", "repository:r_b"},
	}

	req := httptest.NewRequest(http.MethodGet, "/route?repository_id="+requestTestSelector, nil)
	rec := httptest.NewRecorder()
	_, ok := ResolveForRequestWithAccess(rec, req, nil, store, requestTestSelector, access, "code_search.fuzzy_symbol")
	if ok {
		t.Fatal("ResolveForRequestWithAccess() = true, want false for an ambiguous selector")
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var body struct {
		Candidates []AmbiguousCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body = %s (decode err %v)", rec.Body.String(), err)
	}
	if len(body.Candidates) != 2 {
		t.Fatalf("body = %s, want exactly the two readable candidates", rec.Body.String())
	}
	for _, candidate := range body.Candidates {
		if candidate.RepoID == "repository:hidden" {
			t.Fatalf("body = %s, leaks the unreadable repository id", rec.Body.String())
		}
	}
}

func assertRequestSpanError(t *testing.T, span sdktrace.ReadOnlySpan, want bool) {
	t.Helper()
	hasException := false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			hasException = true
		}
	}
	isError := span.Status().Code == codes.Error
	if isError != want || hasException != want {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			span.Status().Code, span.Status().Description, hasException, want)
	}
	if want && span.Status().Description != LookupFailureMessage {
		t.Fatalf("span status description = %q, want the fixed %q", span.Status().Description, LookupFailureMessage)
	}
	if strings.Contains(span.Status().Description, requestTestSelector) {
		t.Fatalf("span status description %q carries the raw selector", span.Status().Description)
	}
}
