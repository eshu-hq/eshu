// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// readerFenceStore fails exactly one Postgres read of the dead-code routes with
// the guarded reader's error so each read site is proven separately. It embeds
// the grant store so every other read succeeds with a real candidate.
type readerFenceStore struct {
	deadCodeGrantContentStore
	failScan     error
	failCoverage error
	failEvidence error
}

func (s *readerFenceStore) DeadCodeCandidateRows(ctx context.Context, q codeshaping.DeadCodeCandidateQuery) ([]map[string]any, error) {
	if s.failScan != nil {
		return nil, s.failScan
	}
	return s.deadCodeGrantContentStore.DeadCodeCandidateRows(ctx, q)
}

func (s *readerFenceStore) RepositoryCoverage(ctx context.Context, repoID string) (querycontract.RepositoryContentCoverage, error) {
	if s.failCoverage != nil {
		return querycontract.RepositoryContentCoverage{}, s.failCoverage
	}
	return s.deadCodeGrantContentStore.RepositoryCoverage(ctx, repoID)
}

func (s *readerFenceStore) CrossRepoDeadCodeConsumerEvidence(
	_ context.Context,
	_ string,
	_ []string,
	_ crossRepoDeadCodeConsumerReads,
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, crossRepoDeadCodeHiddenConsumers, error) {
	if s.failEvidence != nil {
		return nil, nil, s.failEvidence
	}
	return map[string][]deadcode.CrossRepoDeadCodeEvidence{}, crossRepoDeadCodeHiddenConsumers{}, nil
}

// readerFencePrivateText stands in for runtime/postgres privateError: fixed
// public text with the sentinel only reachable through Unwrap.
type readerFencePrivateText struct{ cause error }

func (e readerFencePrivateText) Error() string { return "PostgreSQL reader connection unavailable" }
func (e readerFencePrivateText) Unwrap() error { return e.cause }

// TestDeadCodeRoutesMapReaderFenceFailuresToRetryable503 is the #7523
// regression: a PostgreSQL reader that missed the writer checkpoint, or whose
// connection acquisition or identity check timed out, must reach the client of every dead-code route as a
// retryable 503 backend_unavailable with Retry-After and no Go error text,
// whichever of the route's Postgres reads hit it.
func TestDeadCodeRoutesMapReaderFenceFailuresToRetryable503(t *testing.T) {
	t.Parallel()

	faults := map[string]error{
		"stale": errors.Join(db.ErrReaderStale, context.DeadlineExceeded),
		"pool_wait": readerFencePrivateText{
			cause: errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded),
		},
	}
	routes := []struct {
		name string
		path string
		body map[string]any
		site string
		set  func(*readerFenceStore, error)
	}{
		{"dead_code_scan", "/api/v0/code/dead-code", map[string]any{"language": "go"}, "scan", func(s *readerFenceStore, e error) { s.failScan = e }},
		{"cross_repo_scan", "/api/v0/code/dead-code/cross-repo", map[string]any{"repo_id": codeGrantGrantedRepo, "language": "go"}, "scan", func(s *readerFenceStore, e error) { s.failScan = e }},
		{"cross_repo_evidence", "/api/v0/code/dead-code/cross-repo", map[string]any{"repo_id": codeGrantGrantedRepo, "language": "go"}, "evidence", func(s *readerFenceStore, e error) { s.failEvidence = e }},
		{"investigate_scan", "/api/v0/code/dead-code/investigate", map[string]any{"language": "go"}, "scan", func(s *readerFenceStore, e error) { s.failScan = e }},
		{"investigate_coverage", "/api/v0/code/dead-code/investigate", map[string]any{"repo_id": codeGrantGrantedRepo, "language": "go"}, "coverage", func(s *readerFenceStore, e error) { s.failCoverage = e }},
	}
	for _, route := range routes {
		for faultName, fault := range faults {
			t.Run(route.name+"/"+faultName, func(t *testing.T) {
				t.Parallel()
				store := &readerFenceStore{}
				route.set(store, fmt.Errorf("read %s: %w", route.site, fault))
				handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
				mux := http.NewServeMux()
				handler.Mount(mux)

				req := newCodeGrantRouteRequest(t, route.path, route.body, nil)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)

				assertReaderFenceResponse(t, rec)
			})
		}
	}
}

// assertReaderFenceResponse asserts the stable retryable contract and that no
// Go error text reached the body.
func assertReaderFenceResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("Retry-After"), strconv.Itoa(querycontract.BackendUnavailableRetryAfterSeconds); got != want {
		t.Fatalf("Retry-After = %q, want %q", got, want)
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, rec.Body.String())
	}
	if envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable {
		t.Fatalf("error = %#v, want code backend_unavailable", envelope.Error)
	}
	for _, leak := range []string{"PostgreSQL", "checkpoint", "deadline", "read scan", "read evidence", "read coverage"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("body leaks Go error text %q: %s", leak, rec.Body.String())
		}
	}
}

// TestDeadCodeRoutesStillReturn500ForUnknownStoreErrors proves the reader
// mapping does not swallow an unrelated Postgres failure.
func TestDeadCodeRoutesStillReturn500ForUnknownStoreErrors(t *testing.T) {
	t.Parallel()
	store := &readerFenceStore{failScan: errors.New("boom")}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := newCodeGrantRouteRequest(t, "/api/v0/code/dead-code", map[string]any{"language": "go"}, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatalf("Retry-After set on a 500: %v", rec.Header())
	}
}

// TestDeadCodeRoutesKeep500ForNonTransientReaderFailures: a reader failure that
// carries db.ErrReaderUnavailable but is not a timeout (permission
// denied, connection refused, client cancel) is a permanent or client-side
// condition, so it stays a 500 with no Retry-After and none of the driver text.
func TestDeadCodeRoutesKeep500ForNonTransientReaderFailures(t *testing.T) {
	t.Parallel()
	faults := map[string]error{
		"permission_denied":  errors.New("pq: permission denied for function pg_control_system"),
		"connection_refused": errors.New("dial tcp 10.0.0.9:5432: connect: connection refused"),
		"client_canceled":    context.Canceled,
	}
	for name, cause := range faults {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &readerFenceStore{failScan: readerFencePrivateText{cause: errors.Join(db.ErrReaderUnavailable, cause)}}
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := newCodeGrantRouteRequest(t, "/api/v0/code/dead-code", map[string]any{"language": "go"}, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Retry-After") != "" {
				t.Fatalf("Retry-After set on a 500: %v", rec.Header())
			}
			for _, leak := range []string{"permission denied", "pg_control_system", "connection refused", "canceled", "retry shortly"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("body leaks %q: %s", leak, rec.Body.String())
				}
			}
		})
	}
}
