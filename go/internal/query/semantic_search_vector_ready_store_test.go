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
	lastArgs  []any
	err       error
}

func (q *recordingSearchVectorReadyQueryer) QueryContext(_ context.Context, query string, args ...any) (*sql.Rows, error) {
	q.lastQuery = query
	q.lastArgs = args
	if q.err != nil {
		return nil, q.err
	}
	return nil, errors.New("recorded: no live db in unit test")
}

var testSearchVectorReadyIdentity = SearchVectorBuildIdentity{
	ProviderProfileID:  "semantic-search-default",
	SourceClass:        "search_documents",
	EmbeddingModelID:   "search-embed-v1",
	VectorIndexVersion: "vector-v1",
}

// TestPostgresSearchVectorReadyStoreIssuesWatermarkProbe pins the probe
// contract: the store reports Signaled=true and issues exactly the
// identity-keyed watermark query with the configured identity bound as
// arguments, even when the probe itself errors (so the handler downgrades to
// unavailable rather than falsely reporting fresh).
func TestPostgresSearchVectorReadyStoreIssuesWatermarkProbe(t *testing.T) {
	t.Parallel()

	rec := &recordingSearchVectorReadyQueryer{}
	store := NewPostgresSearchVectorReadyStore(rec, testSearchVectorReadyIdentity)

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
	if len(rec.lastArgs) != 4 {
		t.Fatalf("probe args = %d, want 4 (identity tuple)", len(rec.lastArgs))
	}
	if rec.lastArgs[0] != testSearchVectorReadyIdentity.ProviderProfileID ||
		rec.lastArgs[1] != testSearchVectorReadyIdentity.SourceClass ||
		rec.lastArgs[2] != testSearchVectorReadyIdentity.EmbeddingModelID ||
		rec.lastArgs[3] != testSearchVectorReadyIdentity.VectorIndexVersion {
		t.Fatalf("probe args = %+v, want identity %+v", rec.lastArgs, testSearchVectorReadyIdentity)
	}
}

// TestPostgresSearchVectorReadyStoreRequiresDB proves a nil database is a
// reported error rather than a panic or a silently-fresh signal.
func TestPostgresSearchVectorReadyStoreRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewPostgresSearchVectorReadyStore(nil, testSearchVectorReadyIdentity)
	fr, err := store.SearchVectorReadyWatermark(context.Background())
	if err == nil {
		t.Fatal("expected an error for a nil database")
	}
	if !fr.Signaled {
		t.Fatal("expected Signaled=true so the caller reports unavailable, not silently fresh")
	}
}
