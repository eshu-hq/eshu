// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestPostgresSemanticSearchSnapshotStoreLoadsExactRevisionIdentity(t *testing.T) {
	t.Parallel()

	queryer := &semanticSearchSnapshotQueryer{rows: &semanticSearchSnapshotRows{
		data: [][]any{{"generation-active", int64(7), 500, int64(7), int64(3), "ready"}},
	}}
	store := NewPostgresSemanticSearchSnapshotStore(queryer)
	snapshot, err := store.Load(context.Background(), SemanticSearchSnapshotRequest{
		ScopeID:            " scope-payments ",
		ProviderProfileID:  " local ",
		SourceClass:        " search_documents ",
		EmbeddingModelID:   " local-hash-v1 ",
		VectorIndexVersion: " vector-v1 ",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := snapshot.GenerationID, "generation-active"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := snapshot.DocumentProjectionRevision, int64(7); got != want {
		t.Fatalf("DocumentProjectionRevision = %d, want %d", got, want)
	}
	if got, want := snapshot.VectorBuildFence, int64(3); got != want {
		t.Fatalf("VectorBuildFence = %d, want %d", got, want)
	}
	if !snapshot.Cacheable() {
		t.Fatalf("snapshot = %+v, want cacheable", snapshot)
	}
	if got, want := fmt.Sprint(queryer.args), "[scope-payments local search_documents local-hash-v1 vector-v1]"; got != want {
		t.Fatalf("query args = %s, want %s", got, want)
	}
	for _, fragment := range []string{
		"scope.active_generation_id",
		"projection.state = 'ready'",
		"vector.provider_profile_id = $2",
		"vector.source_class = $3",
		"vector.embedding_model_id = $4",
		"vector.vector_index_version = $5",
		"LIMIT 1",
	} {
		if !strings.Contains(queryer.query, fragment) {
			t.Fatalf("snapshot query missing %q:\n%s", fragment, queryer.query)
		}
	}
}

func TestPostgresSemanticSearchSnapshotStoreReturnsNonCacheableEmptyWhenUnready(t *testing.T) {
	t.Parallel()

	store := NewPostgresSemanticSearchSnapshotStore(&semanticSearchSnapshotQueryer{
		rows: &semanticSearchSnapshotRows{},
	})
	snapshot, err := store.Load(context.Background(), SemanticSearchSnapshotRequest{
		ScopeID:            "scope-payments",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snapshot.Cacheable() {
		t.Fatalf("empty snapshot = %+v, want non-cacheable", snapshot)
	}
}

func TestSemanticSearchSnapshotCacheableRejectsStaleAndPartialStates(t *testing.T) {
	t.Parallel()

	ready := semanticSearchCacheTestSnapshot(4)
	tests := []struct {
		name      string
		mutate    func(*SemanticSearchSnapshot)
		cacheable bool
	}{
		{name: "ready", cacheable: true},
		{name: "missing_generation", mutate: func(s *SemanticSearchSnapshot) { s.GenerationID = "" }},
		{name: "empty", mutate: func(s *SemanticSearchSnapshot) { s.DocumentCount = 0 }},
		{name: "revision_mismatch", mutate: func(s *SemanticSearchSnapshot) { s.VectorProjectionRevision++ }},
		{name: "missing_fence", mutate: func(s *SemanticSearchSnapshot) { s.VectorBuildFence = 0 }},
		{name: "building", mutate: func(s *SemanticSearchSnapshot) { s.VectorState = "building" }},
		{name: "failed", mutate: func(s *SemanticSearchSnapshot) { s.VectorState = "failed" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := ready
			if tc.mutate != nil {
				tc.mutate(&snapshot)
			}
			if got := snapshot.Cacheable(); got != tc.cacheable {
				t.Fatalf("Cacheable() = %t, want %t for %+v", got, tc.cacheable, snapshot)
			}
		})
	}
}

type semanticSearchSnapshotQueryer struct {
	rows  pgstatus.Rows
	query string
	args  []any
}

func (q *semanticSearchSnapshotQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (pgstatus.Rows, error) {
	q.query = query
	q.args = append([]any(nil), args...)
	return q.rows, nil
}

type semanticSearchSnapshotRows struct {
	data [][]any
	idx  int
}

func (r *semanticSearchSnapshotRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *semanticSearchSnapshotRows) Scan(dest ...any) error {
	row := r.data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan arity %d != %d", len(dest), len(row))
	}
	for i, target := range dest {
		switch typed := target.(type) {
		case *string:
			*typed = row[i].(string)
		case *int:
			*typed = row[i].(int)
		case *int64:
			*typed = row[i].(int64)
		default:
			return fmt.Errorf("unsupported scan target %T", target)
		}
	}
	return nil
}

func (r *semanticSearchSnapshotRows) Err() error   { return nil }
func (r *semanticSearchSnapshotRows) Close() error { return nil }
