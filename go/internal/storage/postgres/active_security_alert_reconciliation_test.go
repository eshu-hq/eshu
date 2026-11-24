// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errReadinessQueryer fails every query: the readiness probe and the page
// load both observe the failure.
type errReadinessQueryer struct{ err error }

func (q errReadinessQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, q.err
}

func (q errReadinessQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, q.err
}

// readyThenErrQueryer answers the first query (the supports readiness probe)
// with ready=true and fails every query after it.
type readyThenErrQueryer struct {
	err   error
	calls int
}

func (q *readyThenErrQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	q.calls++
	if q.calls == 1 {
		return &queueFakeRows{rows: [][]any{{true}}}, nil
	}
	return nil, q.err
}

func (q *readyThenErrQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, q.err
}

// TestSecurityAlertReadinessProbeFailureIsLogged is the #10875 review
// regression: a failing readiness probe must emit its signal line instead
// of silently falling back to the legacy query.
func TestSecurityAlertReadinessProbeFailureIsLogged(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// The supports readiness probe (first query) succeeds; every later query,
	// the keys readiness probe and the page load, fails.
	store := FactStore{database: &readyThenErrQueryer{err: errors.New("probe boom")}}
	_, err := store.ListActiveSecurityAlertReconciliationFacts(
		context.Background(),
		securityalert.SecurityAlertReconciliationFactFilter{RepositoryIDs: []string{"acme/app"}},
	)
	if err == nil {
		t.Fatal("ListActiveSecurityAlertReconciliationFacts() error = nil, want the page-load failure")
	}
	line := buf.String()
	if !strings.Contains(line, "event_name=security_alert_reconciliation_keys.readiness_probe_failed") {
		t.Fatalf("log = %q, want the readiness_probe_failed signal line", line)
	}
	if !strings.Contains(line, "repository_filters=1") {
		t.Fatalf("log = %q, want the filter-class counts", line)
	}
}
