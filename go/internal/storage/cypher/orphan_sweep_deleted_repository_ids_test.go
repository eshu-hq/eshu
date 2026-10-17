// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"
)

// TestSweepOrphanNodesReportsDeletedRepositoryIDs proves the #8316 sweep half
// of push invalidation: the keys the cycle actually deleted for the
// Repository label travel on the result as repository ids, so the caller can
// delete the fingerprint rows whose repo_ids contain them. Other labels never
// leak into the repository id list.
func TestSweepOrphanNodesReportsDeletedRepositoryIDs(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	now := time.Now().UTC()
	aged := now.Add(-8 * 24 * time.Hour).Unix()
	recent := now.Add(-time.Hour).Unix()
	graph.seed("Repository", "repo-gone", false, &aged)
	graph.seed("Repository", "repo-connected", true, &aged)
	graph.seed("Repository", "repo-recent", false, &recent)
	graph.seed("File", "/gone-file", false, &aged)

	store := NewOrphanSweepStore(graph, graph)
	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		Labels: []string{"Repository", "File"},
		Now:    now,
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v", err)
	}
	if got := result.Deleted["Repository"]; got != 1 {
		t.Fatalf("Deleted[Repository] = %d, want 1", got)
	}
	got := append([]string(nil), result.DeletedRepositoryIDs...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"repo-gone"}) {
		t.Fatalf("DeletedRepositoryIDs = %#v, want [repo-gone]", got)
	}
}

// TestSweepOrphanNodesReportsNoDeletedRepositoryIDsInSteadyState proves the
// steady state carries no invalidation work: nothing swept, no ids reported.
func TestSweepOrphanNodesReportsNoDeletedRepositoryIDsInSteadyState(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("Repository", "repo-live", true, nil)
	store := NewOrphanSweepStore(graph, graph)
	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		Labels: []string{"Repository"},
		Now:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v", err)
	}
	if len(result.DeletedRepositoryIDs) != 0 {
		t.Fatalf("DeletedRepositoryIDs = %#v, want empty in steady state", result.DeletedRepositoryIDs)
	}
}
