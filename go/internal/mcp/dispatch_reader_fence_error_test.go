// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/iac"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// readerFenceDeadCodeContent fails the dead-code candidate scan the way a
// guarded PostgreSQL reader does when its replica has not caught up.
type readerFenceDeadCodeContent struct {
	content.FakePortContentStore
	err error
}

func (s readerFenceDeadCodeContent) DeadCodeCandidateRows(context.Context, codeshaping.DeadCodeCandidateQuery) ([]map[string]any, error) {
	return nil, s.err
}

// readerFenceReachability fails the materialized IaC reachability reads.
type readerFenceReachability struct{ err error }

func (s readerFenceReachability) ListLatestCleanupFindings(context.Context, []string, []string, bool, int, int) ([]iac.ReachabilityFindingRow, error) {
	return nil, s.err
}

func (s readerFenceReachability) CountLatestCleanupFindings(context.Context, []string, []string, bool) (int, error) {
	return 0, s.err
}

func (s readerFenceReachability) HasLatestRows(context.Context, []string, []string) (bool, error) {
	return false, s.err
}

// TestDispatchToolSurfacesReaderFenceFailuresAsRetryableEnvelope proves #7523
// for MCP: a stale or pool-timed-out PostgreSQL reader under a dead-code or
// dead-IaC tool reaches the client as an isError result whose envelope carries
// the stable backend_unavailable code, the retry hint, and no Go error text.
func TestDispatchToolSurfacesReaderFenceFailuresAsRetryableEnvelope(t *testing.T) {
	t.Parallel()

	stale := fmt.Errorf("scan: %w", errors.Join(db.ErrReaderStale, context.DeadlineExceeded))
	mux := func(mount func(*http.ServeMux)) http.Handler {
		m := http.NewServeMux()
		mount(m)
		return m
	}
	tests := []struct {
		tool    string
		args    map[string]any
		handler http.Handler
	}{
		{
			tool: "find_dead_code",
			args: map[string]any{},
			handler: mux(func(m *http.ServeMux) {
				(&query.CodeHandler{Content: readerFenceDeadCodeContent{err: stale}, Profile: query.ProfileLocalAuthoritative}).Mount(m)
			}),
		},
		{
			tool: "find_dead_iac",
			args: map[string]any{"repo_id": "repo://acme/infra"},
			handler: mux(func(m *http.ServeMux) {
				(&iac.Handler{Reachability: readerFenceReachability{err: stale}}).Mount(m)
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.tool, func(t *testing.T) {
			t.Parallel()
			result, err := dispatchTool(context.Background(), test.handler, test.tool, test.args, "",
				slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatalf("dispatchTool() error = %v, want an isError envelope result", err)
			}
			if result == nil || !result.IsError || result.Envelope == nil || result.Envelope.Error == nil {
				t.Fatalf("result = %#v, want isError with an error envelope", result)
			}
			got := result.Envelope.Error
			if got.Code != query.ErrorCodeBackendUnavailable {
				t.Fatalf("error code = %q, want %q", got.Code, query.ErrorCodeBackendUnavailable)
			}
			if got.Details["retry_after_seconds"] != float64(querycontract.BackendUnavailableRetryAfterSeconds) {
				t.Fatalf("details = %#v, want retry_after_seconds", got.Details)
			}
			for _, leak := range []string{"PostgreSQL", "checkpoint", "deadline", "scan:"} {
				if strings.Contains(got.Message, leak) {
					t.Fatalf("message leaks Go error text %q: %s", leak, got.Message)
				}
			}
		})
	}
}

// TestDispatchToolKeepsNonTransientReaderFailuresOutOfRetryableEnvelope: a
// reader failure that carries db.ErrReaderUnavailable without a pool-wait
// deadline (permission denied, connection refused, client cancel) is not
// retryable, so MCP must not advertise backend_unavailable or a retry hint, and
// must not show the driver text (#7523 review).
func TestDispatchToolKeepsNonTransientReaderFailuresOutOfRetryableEnvelope(t *testing.T) {
	t.Parallel()

	causes := map[string]error{
		"permission_denied":  errors.New("pq: permission denied for function pg_control_system"),
		"connection_refused": errors.New("dial tcp 10.0.0.9:5432: connect: connection refused"),
		"client_canceled":    context.Canceled,
	}
	for name, cause := range causes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			failure := privateReaderError{cause: errors.Join(db.ErrReaderUnavailable, cause)}
			m := http.NewServeMux()
			(&iac.Handler{Reachability: readerFenceReachability{err: failure}}).Mount(m)

			result, err := dispatchTool(context.Background(), m, "find_dead_iac",
				map[string]any{"repo_id": "repo://acme/infra"}, "",
				slog.New(slog.NewTextHandler(io.Discard, nil)))
			if result != nil && result.Envelope != nil && result.Envelope.Error != nil {
				t.Fatalf("result = %#v, want the plain 500 dispatch error, not a retryable envelope", result.Envelope.Error)
			}
			if err == nil {
				t.Fatalf("dispatchTool() error = nil, want the HTTP 500 dispatch error")
			}
			text := err.Error()
			if !strings.Contains(text, "HTTP 500") {
				t.Fatalf("error = %q, want an HTTP 500", text)
			}
			for _, leak := range []string{
				"permission denied", "pg_control_system", "connection refused", "canceled",
				"retry shortly", "backend_unavailable", "retry_after_seconds",
			} {
				if strings.Contains(text, leak) {
					t.Fatalf("error leaks or advertises %q: %s", leak, text)
				}
			}
		})
	}
}

// privateReaderError stands in for runtime/postgres privateError: a fixed
// public text with the cause reachable only through Unwrap.
type privateReaderError struct{ cause error }

func (e privateReaderError) Error() string { return "PostgreSQL reader connection unavailable" }
func (e privateReaderError) Unwrap() error { return e.cause }
