// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type recordingAdminQueryer struct{ queries []string }

func (q *recordingAdminQueryer) QueryContext(_ context.Context, sql string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, sql)
	return &recordingAdminRows{}, nil
}

func TestReadStoreUsesQueryOnlyPort(t *testing.T) {
	t.Parallel()
	queries := &recordingAdminQueryer{}
	reader := NewReadStore(queries)
	ctx := context.Background()
	if _, err := reader.ListWorkItems(ctx, admin.WorkItemFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListDeadLetterWorkItems(ctx, admin.DeadLetterListFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListReducerInputInvalidFacts(ctx, admin.InputInvalidFactListFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListChangedSincePoisonedLinks(ctx, admin.ChangedSincePoisonedLinkFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListReplayEvents(ctx, admin.ReplayEventFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListDecisions(ctx, admin.DecisionQueryFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListEvidence(ctx, "decision"); err != nil {
		t.Fatal(err)
	}
	if len(queries.queries) != 7 {
		t.Fatalf("queries = %d, want 7", len(queries.queries))
	}
	for _, method := range []string{"DeadLetterWorkItems", "SkipRepositoryWorkItems", "ReplayFailedWorkItems", "RequestBackfill", "ClaimReplayIdempotency"} {
		if _, exists := reflect.TypeOf(reader).MethodByName(method); exists {
			t.Fatalf("reader exposes mutation %s", method)
		}
	}
	if NewReadStore(nil) != nil {
		t.Fatal("nil queryer must yield nil store")
	}
}
