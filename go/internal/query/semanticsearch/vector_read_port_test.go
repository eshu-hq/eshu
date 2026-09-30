// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingVectorReader struct {
	calls int
	err   error
}

func (r *rejectingVectorReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestVectorReadyGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingVectorReader{err: want}
	store := NewPostgresSearchVectorReadyStoreWithReadStore(guard, testSearchVectorReadyIdentity)
	_, err := store.SearchVectorReadyWatermark(t.Context())
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.calls)
	}
}

func TestSemanticSearchIndexGuardedReadPort(t *testing.T) {
	want := errors.New("reader stale")
	reader := &rejectingVectorReader{err: want}
	store := NewPostgresSemanticSearchIndexStoreWithReadStore(reader)
	_, err := store.ListActiveDocuments(t.Context(), semanticSearchDocumentQuery{ScopeID: "scope", Limit: 1})
	if !errors.Is(err, want) || reader.calls != 1 {
		t.Fatalf("error %v, calls %d", err, reader.calls)
	}
}

func TestSemanticSearchIndexNilLegacyDatabase(t *testing.T) {
	store := NewPostgresSemanticSearchIndexStore(nil)
	if _, err := store.ListActiveDocuments(t.Context(), semanticSearchDocumentQuery{ScopeID: "scope", Limit: 1}); err == nil {
		t.Fatal("expected missing database error")
	}
}
