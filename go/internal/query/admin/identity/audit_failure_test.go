// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestAdminAuditReadsAnswerFixedServerFailures is the #7674 regression for
// GET /api/v0/auth/admin/audit/events and GET /api/v0/auth/admin/audit/summary.
// Both read the governance audit store through the guarded PostgreSQL reader,
// but answered a stale reader with their fixed 500 instead of the retryable
// 503, and answered a client cancel as a server fault. The fence envelope
// names each route's capability-matrix key (#8227).
func TestAdminAuditReadsAnswerFixedServerFailures(t *testing.T) {
	t.Parallel()

	sharedOperator := func(ctx context.Context) context.Context {
		return auth.ContextWithAuthContext(ctx, auth.AuthContext{Mode: auth.AuthModeShared, AllScopes: true})
	}
	wantCapability := map[string]string{
		"audit events":  "audit.events.list",
		"audit summary": "audit.summary.read",
	}
	routes := []failure.Route{
		{
			Name: "audit events", Path: "/api/v0/auth/admin/audit/events?limit=10",
			Message: auditEventsListFailedMessage, Scope: sharedOperator,
			Handler: func(err error) failure.Mounter {
				return &ReadHandler{Audit: failingAuditReader{err: err}}
			},
		},
		{
			Name: "audit summary", Path: "/api/v0/auth/admin/audit/summary",
			Message: auditSummaryFailedMessage, Scope: sharedOperator,
			Handler: func(err error) failure.Mounter {
				return &ReadHandler{Audit: failingAuditReader{err: err}}
			},
		},
	}
	suite := failure.Suite{Routes: routes}
	failure.Run(t, suite)

	for _, route := range routes {
		rec, _ := failure.Serve(suite, route, route.Handler(fmt.Errorf("%s: %w", failure.Canary, db.ErrReaderStale)), false)
		var body struct {
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode fence body: %v; body = %s", route.Name, err, rec.Body.String())
		}
		if got := body.Error["capability"]; got != wantCapability[route.Name] {
			t.Errorf("%s: fence envelope capability = %v, want %q", route.Name, got, wantCapability[route.Name])
		}
	}
}

// TestAdminAuditReadsLogOnlyServerFaults pins the audit reads' log decision
// (#7674): a server fault writes one error log line naming the read, and a
// client cancel writes none, because the client walking away is not a
// failure. Not parallel: it swaps the default slog logger.
func TestAdminAuditReadsLogOnlyServerFaults(t *testing.T) {
	records := &capturedLogRecords{}
	previous := slog.Default()
	slog.SetDefault(slog.New(records))
	t.Cleanup(func() { slog.SetDefault(previous) })

	sharedOperator := func(ctx context.Context) context.Context {
		return auth.ContextWithAuthContext(ctx, auth.AuthContext{Mode: auth.AuthModeShared, AllScopes: true})
	}
	for _, tc := range []struct {
		name, path, logMessage string
	}{
		{name: "audit events", path: "/api/v0/auth/admin/audit/events?limit=10", logMessage: "admin list audit events failed"},
		{name: "audit summary", path: "/api/v0/auth/admin/audit/summary", logMessage: "admin audit summary failed"},
	} {
		route := failure.Route{Name: tc.name, Path: tc.path, Scope: sharedOperator}
		for _, kase := range []struct {
			name    string
			err     error
			cancel  bool
			wantLog int
		}{
			{name: "server fault", err: errors.New("backend: " + failure.Canary), wantLog: 1},
			{name: "client cancel", err: fmt.Errorf("backend: %w", context.Canceled), cancel: true, wantLog: 0},
		} {
			records.reset()
			failure.Serve(failure.Suite{}, route, &ReadHandler{Audit: failingAuditReader{err: kase.err}}, kase.cancel)
			if got := records.count(slog.LevelError, tc.logMessage); got != kase.wantLog {
				t.Errorf("%s / %s: %d error log line(s) %q, want %d", tc.name, kase.name, got, tc.logMessage, kase.wantLog)
			}
		}
	}
}

// capturedLogRecords is a slog.Handler that keeps every record's level and
// message for TestAdminAuditReadsLogOnlyServerFaults.
type capturedLogRecords struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *capturedLogRecords) Enabled(context.Context, slog.Level) bool { return true }

func (c *capturedLogRecords) Handle(_ context.Context, record slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, record)
	return nil
}

func (c *capturedLogRecords) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *capturedLogRecords) WithGroup(string) slog.Handler { return c }

func (c *capturedLogRecords) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = nil
}

func (c *capturedLogRecords) count(level slog.Level, message string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, record := range c.records {
		if record.Level == level && record.Message == message {
			n++
		}
	}
	return n
}

// failingAuditReader fails every audit read with err, unconditionally.
type failingAuditReader struct{ err error }

func (f failingAuditReader) ListAuditEvents(context.Context, AuditQuery) ([]governanceaudit.Event, error) {
	return nil, f.err
}

func (f failingAuditReader) SummarizeAuditEvents(context.Context) (governanceaudit.Summary, error) {
	return governanceaudit.Summary{}, f.err
}

func (f failingAuditReader) SummarizeAuditEventsForTenant(context.Context, string) (governanceaudit.Summary, error) {
	return governanceaudit.Summary{}, f.err
}
