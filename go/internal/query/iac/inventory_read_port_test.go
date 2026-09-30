// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingInventoryReader struct {
	calls int
	err   error
}

func (r *rejectingInventoryReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestInventoryGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingInventoryReader{err: want}
	store := NewPostgresIaCInventoryStoreWithReadStore(guard)
	_, err := store.SearchActive(t.Context(), InventorySearch{Kind: resourceKindResource, Limit: 1}, querycontract.RepositoryAccessFilter{})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one readiness query", err, guard.calls)
	}
}

func TestReachabilityGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingInventoryReader{err: want}
	store := NewPostgresIaCReachabilityStoreWithReadStore(guard)
	_, err := store.ListLatestCleanupFindings(t.Context(), []string{"repo"}, nil, false, 1, 0)
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, calls %d", err, guard.calls)
	}
}

func TestManagementGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingInventoryReader{err: want}
	store := NewPostgresIaCManagementStoreWithReadStore(guard)
	_, err := store.ListReplatformingSelectors(t.Context(), 1, nil)
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, calls %d", err, guard.calls)
	}
}
