// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestPostgresCollectorListReadinessRequiresDB(t *testing.T) {
	t.Parallel()

	store := PostgresCollectorListReadinessStore{}
	if _, err := store.CollectorConfigured(context.Background(), scope.CollectorPackageRegistry); err == nil {
		t.Fatal("CollectorConfigured() error = nil, want error for nil DB")
	}
}

type recordingCollectorListReadinessQueryer struct {
	called int
	query  string
	args   []any
	err    error
}

func (r *recordingCollectorListReadinessQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (*sql.Rows, error) {
	r.called++
	r.query = query
	r.args = args
	// Returning a transport error is enough to exercise the call path without a
	// real Postgres; the row-scan path is covered by integration tests.
	if r.err != nil {
		return nil, r.err
	}
	return nil, fmt.Errorf("no rows available in fake")
}

func TestPostgresCollectorListReadinessProbesCollectorInstancesByKind(t *testing.T) {
	t.Parallel()

	db := &recordingCollectorListReadinessQueryer{}
	store := NewPostgresCollectorListReadinessStore(db)
	_, _ = store.CollectorConfigured(context.Background(), scope.CollectorCICDRun)

	if db.called != 1 {
		t.Fatalf("QueryContext invocations = %d, want 1", db.called)
	}
	if !strings.Contains(db.query, "FROM collector_instances") {
		t.Fatalf("probe query missing collector_instances source:\n%s", db.query)
	}
	if !strings.Contains(db.query, "enabled = TRUE") {
		t.Fatalf("probe query missing enabled predicate:\n%s", db.query)
	}
	if !strings.Contains(db.query, "deactivated_at IS NULL") {
		t.Fatalf("probe query missing deactivated_at predicate:\n%s", db.query)
	}
	if len(db.args) != 1 || db.args[0] != string(scope.CollectorCICDRun) {
		t.Fatalf("probe args = %v, want [%q]", db.args, scope.CollectorCICDRun)
	}
}

func TestPostgresCollectorListReadinessWrapsQueryError(t *testing.T) {
	t.Parallel()

	db := &recordingCollectorListReadinessQueryer{err: fmt.Errorf("boom")}
	store := NewPostgresCollectorListReadinessStore(db)
	if _, err := store.CollectorConfigured(context.Background(), scope.CollectorOCIRegistry); err == nil {
		t.Fatal("CollectorConfigured() error = nil, want wrapped query error")
	}
}
