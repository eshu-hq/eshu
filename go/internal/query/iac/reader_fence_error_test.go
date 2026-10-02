// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// readerFenceReachability fails each materialized-reachability read with err
// when its site is selected.
type readerFenceReachability struct {
	site string
	err  error
}

func (s readerFenceReachability) fail(site string) error {
	if s.site == site {
		return s.err
	}
	return nil
}

func (s readerFenceReachability) ListLatestCleanupFindings(
	context.Context, []string, []string, bool, int, int,
) ([]ReachabilityFindingRow, error) {
	return nil, s.fail("list")
}

func (s readerFenceReachability) CountLatestCleanupFindings(
	context.Context, []string, []string, bool,
) (int, error) {
	return 0, s.fail("count")
}

func (s readerFenceReachability) HasLatestRows(context.Context, []string, []string) (bool, error) {
	return false, s.fail("has_rows")
}

// readerFenceContent fails the two content reads handleDeadIaC makes.
type readerFenceContent struct {
	content.FakePortContentStore
	matchErr error
	filesErr error
}

func (s readerFenceContent) MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error) {
	if s.matchErr != nil {
		return nil, s.matchErr
	}
	return s.FakePortContentStore.MatchRepositories(ctx, selector)
}

func (s readerFenceContent) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	if s.filesErr != nil {
		return nil, s.filesErr
	}
	return s.FakePortContentStore.ListRepoFiles(ctx, repoID, limit)
}

// readerFencePrivateText stands in for runtime/postgres privateError: fixed
// public text with the sentinel only reachable through Unwrap.
type readerFencePrivateText struct{ cause error }

func (e readerFencePrivateText) Error() string { return "PostgreSQL reader connection unavailable" }
func (e readerFencePrivateText) Unwrap() error { return e.cause }

// TestHandleDeadIaCMapsReaderFenceFailuresToRetryable503 is the #7523
// regression for POST /api/v0/iac/dead: every Postgres read the route makes
// answers a stale or pool-timed-out reader with a retryable 503, a Retry-After
// header, and no Go error text.
func TestHandleDeadIaCMapsReaderFenceFailuresToRetryable503(t *testing.T) {
	t.Parallel()

	faults := map[string]error{
		"stale": errors.Join(db.ErrReaderStale, context.DeadlineExceeded),
		"pool_wait": readerFencePrivateText{
			cause: errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded),
		},
	}
	sites := []struct {
		name    string
		repoID  string
		handler func(err error) *Handler
	}{
		{"count", "repo://acme/infra", func(err error) *Handler {
			return &Handler{Reachability: readerFenceReachability{site: "count", err: err}}
		}},
		{"list", "repo://acme/infra", func(err error) *Handler {
			return &Handler{Reachability: readerFenceReachability{site: "list", err: err}}
		}},
		{"has_rows", "repo://acme/infra", func(err error) *Handler {
			return &Handler{Reachability: readerFenceReachability{site: "has_rows", err: err}}
		}},
		{"content_files", "repo://acme/infra", func(err error) *Handler {
			return &Handler{Content: readerFenceContent{filesErr: err}}
		}},
		{"selector_match", "infra-repo-name", func(err error) *Handler {
			return &Handler{Content: readerFenceContent{matchErr: err}}
		}},
	}
	for _, site := range sites {
		for faultName, fault := range faults {
			t.Run(site.name+"/"+faultName, func(t *testing.T) {
				t.Parallel()
				mux := http.NewServeMux()
				site.handler(fmt.Errorf("read %s: %w", site.name, fault)).Mount(mux)
				req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead",
					bytes.NewBufferString(fmt.Sprintf(`{"repo_id":%q}`, site.repoID)))
				req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
				rec := httptest.NewRecorder()

				mux.ServeHTTP(rec, req)

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
				for _, leak := range []string{"PostgreSQL", "checkpoint", "deadline", "read " + site.name} {
					if strings.Contains(rec.Body.String(), leak) {
						t.Fatalf("body leaks Go error text %q: %s", leak, rec.Body.String())
					}
				}
			})
		}
	}
}

// TestHandleDeadIaCStillReturns500ForUnknownStoreErrors proves the reader
// mapping does not claim an unrelated Postgres failure.
func TestHandleDeadIaCStillReturns500ForUnknownStoreErrors(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	(&Handler{Reachability: readerFenceReachability{site: "count", err: errors.New("boom")}}).Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{"repo_id":"repo://acme/infra"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatalf("Retry-After set on a 500: %v", rec.Header())
	}
}

// TestHandleDeadIaCKeeps500ForNonTransientReaderFailures: db.ErrReaderUnavailable
// without a pool-wait deadline (permission denied, connection refused, client
// cancel) is not retryable, so the route answers 500 with no Retry-After and no
// driver text (#7523 review).
func TestHandleDeadIaCKeeps500ForNonTransientReaderFailures(t *testing.T) {
	t.Parallel()
	faults := map[string]error{
		"permission_denied":  errors.New("pq: permission denied for function pg_control_system"),
		"connection_refused": errors.New("dial tcp 10.0.0.9:5432: connect: connection refused"),
		"client_canceled":    context.Canceled,
	}
	for name, cause := range faults {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := readerFencePrivateText{cause: errors.Join(db.ErrReaderUnavailable, cause)}
			mux := http.NewServeMux()
			(&Handler{Reachability: readerFenceReachability{site: "count", err: err}}).Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{"repo_id":"repo://acme/infra"}`))
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
