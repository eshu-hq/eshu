// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingWorkItemReader struct {
	calls int
	err   error
}

func (r *rejectingWorkItemReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestWorkItemGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingWorkItemReader{err: want}
	store := NewPostgresEvidenceStoreWithReadStore(guard)
	_, err := store.ListWorkItemEvidence(t.Context(), EvidenceFilter{ScopeID: "scope", Limit: 1})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.calls)
	}
}
