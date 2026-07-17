// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"testing"
)

func TestPersistedSemanticSearchRetriesWhenSnapshotChangesDuringBuild(t *testing.T) {
	t.Parallel()

	hybrid, documents, metadata, values, snapshots := newSemanticSearchCacheTestHybrid(t, 2)
	revision1 := semanticSearchCacheTestSnapshot(1)
	revision2 := semanticSearchCacheTestSnapshot(2)
	snapshots.setSequence(revision1, revision2, revision2, revision2)

	result, err := hybrid.Search(context.Background(), semanticSearchCacheTestQuery("refund"))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("Search() returned no candidates after snapshot retry")
	}
	if got, want := documents.callCount(), 2; got != want {
		t.Fatalf("document loads = %d, want %d after changed-snapshot retry", got, want)
	}
	if got, want := metadata.callCount(), 2; got != want {
		t.Fatalf("metadata loads = %d, want %d after changed-snapshot retry", got, want)
	}
	if got, want := values.callCount(), 2; got != want {
		t.Fatalf("vector loads = %d, want %d after changed-snapshot retry", got, want)
	}
	if got, want := snapshots.callCount(), 4; got != want {
		t.Fatalf("snapshot loads = %d, want %d across retry", got, want)
	}
}

func TestPersistedSemanticSearchBypassesCacheForUnreadySnapshot(t *testing.T) {
	t.Parallel()

	hybrid, documents, _, _, snapshots := newSemanticSearchCacheTestHybrid(t, 2)
	unready := semanticSearchCacheTestSnapshot(1)
	unready.VectorState = "building"
	snapshots.setSequence(unready)
	query := semanticSearchCacheTestQuery("refund")

	first, err := hybrid.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("first Search() error = %v", err)
	}
	second, err := hybrid.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("second Search() error = %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("bypassed Search() = %#v, want exact first result %#v", second, first)
	}
	if got, want := documents.callCount(), 2; got != want {
		t.Fatalf("document loads = %d, want %d without caching unready state", got, want)
	}
	if got, want := snapshots.callCount(), 2; got != want {
		t.Fatalf("snapshot loads = %d, want %d", got, want)
	}
}
