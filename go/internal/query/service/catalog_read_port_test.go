// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingCatalogReader struct {
	calls int
	err   error
}

func (r *rejectingCatalogReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestCatalogGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingCatalogReader{err: want}
	store := NewPostgresServiceCatalogCorrelationStoreWithReadStore(guard)
	_, err := store.ListServiceCatalogCorrelations(t.Context(), CatalogCorrelationFilter{ScopeID: "scope", Limit: 1})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.calls)
	}
}
