// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditstore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

// auditQueries exposes only reads from the existing tenant-aware fixture.
type auditQueries struct {
	inner db.Queryer
	calls int
}

func (q *auditQueries) QueryContext(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	q.calls++
	return q.inner.QueryContext(ctx, sql, args...)
}

func TestGovernanceAuditReaderUsesQueryOnlyPort(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := newGovernanceAuditTenantMemoryDB()
	writer := NewGovernanceAuditStore(database)
	events := []governanceaudit.Event{
		governanceAuditTenantEvent("tenant_a", "", governanceAuditTenantTestTime()),
		governanceAuditTenantEvent("tenant_b", "", governanceAuditTenantTestTime()),
	}
	if err := writer.Append(ctx, events); err != nil {
		t.Fatal(err)
	}
	queries := &auditQueries{inner: database}
	reader := NewGovernanceAuditReader(queries)
	if _, err := reader.List(ctx, GovernanceAuditQuery{}); !errors.Is(err, ErrGovernanceAuditQueryUnauthorized) {
		t.Fatalf("unauthorized List = %v", err)
	}
	if queries.calls != 0 {
		t.Fatal("unauthorized List issued SQL")
	}
	filter := GovernanceAuditQuery{OperatorAuthorized: true, TenantID: "tenant_a"}
	got, err := reader.List(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	want, err := writer.List(ctx, filter)
	if err != nil || !reflect.DeepEqual(got, want) || len(got) != 1 {
		t.Fatalf("List parity: got %v, want %v, error %v", got, want, err)
	}
	for _, name := range []string{"Append", "EnsureSchema", "DeleteExpired"} {
		if _, exists := reflect.TypeOf(reader).MethodByName(name); exists {
			t.Fatalf("reader exposes write method %s", name)
		}
	}
}

func TestGovernanceAuditReaderMissingPort(t *testing.T) {
	t.Parallel()
	reader := NewGovernanceAuditReader(nil)
	ctx := context.Background()
	if _, err := reader.List(ctx, GovernanceAuditQuery{OperatorAuthorized: true}); err == nil {
		t.Fatal("List accepted nil reader")
	}
	if _, err := reader.Summary(ctx); err == nil {
		t.Fatal("Summary accepted nil reader")
	}
	if _, err := reader.SummaryForTenant(ctx, "tenant_a"); err == nil {
		t.Fatal("SummaryForTenant accepted nil reader")
	}
}

func TestGovernanceAuditReaderSummariesUseQueryOnlyPort(t *testing.T) {
	t.Parallel()
	for _, tenant := range []string{"", "tenant_a"} {
		t.Run(tenant, func(t *testing.T) {
			database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{
				{"total", "", int64(2), governanceAuditTenantTestTime()},
				{"decision", string(governanceaudit.DecisionDenied), int64(1), governanceAuditTenantTestTime()},
			}}}}
			reader := NewGovernanceAuditReader(&auditQueries{inner: database})
			var got governanceaudit.Summary
			var err error
			if tenant == "" {
				got, err = reader.Summary(context.Background())
			} else {
				got, err = reader.SummaryForTenant(context.Background(), tenant)
			}
			if err != nil || got.Total != 2 || got.Denied != 1 {
				t.Fatalf("summary = %+v, error = %v", got, err)
			}
			if len(database.Queries) != 1 {
				t.Fatal("expected one summary query")
			}
			args := database.Queries[0].Args
			if tenant == "" && len(args) != 0 {
				t.Fatalf("unscoped args = %v", args)
			}
			if tenant != "" && (len(args) != 1 || args[0] != tenant) {
				t.Fatalf("tenant args = %v", args)
			}
		})
	}
}
