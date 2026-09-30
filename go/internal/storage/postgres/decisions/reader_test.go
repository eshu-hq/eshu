// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package decisionsstore

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingDecisionQueryer struct {
	calls int
	err   error
}

func (q *rejectingDecisionQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	q.calls++
	return nil, q.err
}

func TestDecisionReaderUsesQueryOnlyPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingDecisionQueryer{err: want}
	reader := NewDecisionReader(guard)
	_, err := reader.ListDecisions(context.Background(), DecisionFilter{
		RepositoryID: "repo", SourceRunID: "run", Limit: 1,
	})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("read error = %v, guarded queries = %d; want stale reader and one query", err, guard.calls)
	}
}
