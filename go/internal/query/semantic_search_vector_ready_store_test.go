// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type recordingSearchVectorReadyQueryer struct {
	lastQuery string
	err       error
}

func (q *recordingSearchVectorReadyQueryer) QueryContext(_ context.Context, query string, _ ...any) (*sql.Rows, error) {
	q.lastQuery = query
	if q.err != nil {
		return nil, q.err
	}
	return nil, errors.New("recorded: no live db in unit test")
}

// TestPostgresSearchVectorReadyStoreIssuesWatermarkProbe pins the probe
// contract: the store reports Signaled=true and issues exactly the singleton
// watermark query, even when the probe itself errors (so the handler
// downgrades to unavailable rather than falsely reporting fresh).
func TestPostgresSearchVectorReadyStoreIssuesWatermarkProbe(t *testing.T) {
	t.Parallel()

	rec := &recordingSearchVectorReadyQueryer{}
	store := NewPostgresSearchVectorReadyStore(rec)

	fr, err := store.SearchVectorReadyWatermark(context.Background())

	if err == nil {
		t.Fatal("expected the recorded probe error to propagate")
	}
	if !fr.Signaled {
		t.Fatal("expected Signaled=true even when the probe errors")
	}
	if rec.lastQuery != selectSearchVectorReadyWatermarkQuery {
		t.Fatalf("issued the wrong probe query: %q", rec.lastQuery)
	}
}

// TestPostgresSearchVectorReadyStoreRequiresDB proves a nil database is a
// reported error rather than a panic or a silently-fresh signal.
func TestPostgresSearchVectorReadyStoreRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewPostgresSearchVectorReadyStore(nil)
	fr, err := store.SearchVectorReadyWatermark(context.Background())
	if err == nil {
		t.Fatal("expected an error for a nil database")
	}
	if !fr.Signaled {
		t.Fatal("expected Signaled=true so the caller reports unavailable, not silently fresh")
	}
}
