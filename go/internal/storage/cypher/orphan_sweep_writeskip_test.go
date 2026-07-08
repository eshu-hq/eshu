// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOrphanSweepStoreSkipsAllWritesWhenNothingToDo(t *testing.T) {
	t.Parallel()

	executor := &recordingOrphanSweepExecutor{}
	reader := &countingOrphanSweepReader{
		markedCount: 0,
		orphanCount: 0,
		agedCount:   0,
	}
	store := NewOrphanSweepStore(executor, reader)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all writes should be skipped)", got)
	}
	if got := result.Counts["Repository"]; got != 0 {
		t.Fatalf("Repository count = %d, want 0", got)
	}
	if got := result.Marked["Repository"]; got != 0 {
		t.Fatalf("Repository marked = %d, want 0", got)
	}
	if got := result.Deleted["Repository"]; got != 0 {
		t.Fatalf("Repository deleted = %d, want 0", got)
	}
	if got := result.Skipped["Repository"]; got != 3 {
		t.Fatalf("Repository skipped = %d, want 3 (clear+mark+sweep all skipped)", got)
	}
}

func TestOrphanSweepStoreRunsMarkWhenOrphansPresentButSkipsClearSweepWhenNoMarkers(t *testing.T) {
	t.Parallel()

	executor := &recordingOrphanSweepExecutor{}
	reader := &countingOrphanSweepReader{
		markedCount: 0,
		orphanCount: 5,
		agedCount:   0,
	}
	store := NewOrphanSweepStore(executor, reader)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(executor.calls); got != 1 {
		t.Fatalf("executor calls = %d, want 1 (only mark should execute)", got)
	}
	if !strings.Contains(executor.calls[0].Cypher, "SET n.eshu_orphan_observed_at_unix") {
		t.Fatalf("expected mark statement, got: %s", executor.calls[0].Cypher)
	}
	if got := result.Counts["Repository"]; got != 5 {
		t.Fatalf("Repository count = %d, want 5", got)
	}
	if got := result.Marked["Repository"]; got != 2 {
		t.Fatalf("Repository marked = %d, want bounded 2", got)
	}
	if got := result.Deleted["Repository"]; got != 0 {
		t.Fatalf("Repository deleted = %d, want 0", got)
	}
	if got := result.Skipped["Repository"]; got != 2 {
		t.Fatalf("Repository skipped = %d, want 2 (clear+sweep skipped)", got)
	}
}

func TestOrphanSweepStoreRunsClearAndSweepWhenMarkersPresent(t *testing.T) {
	t.Parallel()

	executor := &recordingOrphanSweepExecutor{}
	reader := &countingOrphanSweepReader{
		markedCount:   2,
		orphanCount:   0,
		agedCount:     3,
		unmarkedCount: 0,
		relinkedCount: 2,
	}
	store := NewOrphanSweepStore(executor, reader)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(executor.calls); got != 2 {
		t.Fatalf("executor calls = %d, want 2 (clear+sweep)", got)
	}
	if !strings.Contains(executor.calls[0].Cypher, "REMOVE n.eshu_orphan_observed_at_unix") {
		t.Fatalf("expected clear statement, got: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "DELETE n") {
		t.Fatalf("expected sweep statement, got: %s", executor.calls[1].Cypher)
	}
	if got := result.Counts["Repository"]; got != 0 {
		t.Fatalf("Repository count = %d, want 0", got)
	}
	if got := result.Marked["Repository"]; got != 0 {
		t.Fatalf("Repository marked = %d, want 0 (mark skipped)", got)
	}
	if got := result.Deleted["Repository"]; got != 2 {
		t.Fatalf("Repository deleted = %d, want bounded 2", got)
	}
	if got := result.Skipped["Repository"]; got != 1 {
		t.Fatalf("Repository skipped = %d, want 1 (mark skipped)", got)
	}
}

func TestBuildCountMarkedOrphanNodesQueryIsLabelScopedAndBounded(t *testing.T) {
	t.Parallel()

	stmt, ok := BuildCountMarkedOrphanNodesQuery(OrphanSweepLabelRepository, 500)
	if !ok {
		t.Fatal("BuildCountMarkedOrphanNodesQuery() ok = false, want true")
	}
	for _, want := range []string{
		"MATCH (n:Repository)",
		"n.eshu_orphan_observed_at_unix IS NOT NULL",
		"LIMIT $limit",
		"RETURN count(n) AS orphan_count",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("marked-count Cypher missing %q:\n%s", want, stmt.Cypher)
		}
	}
	if strings.Contains(stmt.Cypher, "NOT (n)--()") {
		t.Fatalf("marked-count Cypher must not guard on zero relationships:\n%s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "<= $cutoff_unix") {
		t.Fatalf("marked-count Cypher must not filter on aged cutoff:\n%s", stmt.Cypher)
	}
	if got := stmt.Parameters["limit"]; got != 500 {
		t.Fatalf("limit = %#v, want 500", got)
	}
}

// TestOrphanSweepStoreSkipsClearWhenMarkedButNotRelinked reproduces the codex
// #4955 finding: a freshly marked orphan (marked, still disconnected, younger
// than the TTL) makes markedCount > 0, but the clear write only matches marked
// AND relinked nodes. Gating clear on marker-presence alone would reissue a
// zero-row ~14s NornicDB write every cycle until the marker ages out; gating on
// the marked+relinked count skips it.
func TestOrphanSweepStoreSkipsClearWhenMarkedButNotRelinked(t *testing.T) {
	t.Parallel()

	executor := &recordingOrphanSweepExecutor{}
	reader := &countingOrphanSweepReader{
		markedCount:   2, // markers exist (freshly marked orphans)
		orphanCount:   2, // still orphans (disconnected)
		agedCount:     0, // not yet past the TTL
		unmarkedCount: 0, // already marked, nothing left to mark
		relinkedCount: 0, // none regained a relationship, nothing to clear
	}
	store := NewOrphanSweepStore(executor, reader)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (clear/mark/sweep all skipped for marked-but-idle orphans)", got)
	}
	if got := result.Skipped["Repository"]; got != 3 {
		t.Fatalf("Repository skipped = %d, want 3", got)
	}
}

// TestOrphanSweepStoreSkipsMarkWhenOrphansAlreadyMarked covers the mark side of
// the same class: total orphans > 0 but every orphan already carries the
// marker, so the mark write matches zero rows. Gating mark on the unmarked
// orphan count (not the total orphan count) skips the zero-row write.
func TestOrphanSweepStoreSkipsMarkWhenOrphansAlreadyMarked(t *testing.T) {
	t.Parallel()

	executor := &recordingOrphanSweepExecutor{}
	reader := &countingOrphanSweepReader{
		markedCount:   5, // markers exist
		orphanCount:   5, // total orphans (all already marked)
		agedCount:     0, // not yet aged
		unmarkedCount: 0, // nothing left to mark
		relinkedCount: 0, // nothing to clear
	}
	store := NewOrphanSweepStore(executor, reader)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (mark skipped because all orphans already marked)", got)
	}
	if got := result.Counts["Repository"]; got != 5 {
		t.Fatalf("Repository count = %d, want 5 (total orphans still reported)", got)
	}
	if got := result.Marked["Repository"]; got != 0 {
		t.Fatalf("Repository marked = %d, want 0", got)
	}
	if got := result.Skipped["Repository"]; got != 3 {
		t.Fatalf("Repository skipped = %d, want 3", got)
	}
}

// TestBuildCountUnmarkedAndMarkedRelinkedQueriesMirrorWritePredicates locks the
// new guard queries to the exact predicates of the mark and clear writes.
func TestBuildCountUnmarkedAndMarkedRelinkedQueriesMirrorWritePredicates(t *testing.T) {
	t.Parallel()

	unmarked, ok := BuildCountUnmarkedOrphanNodesQuery(OrphanSweepLabelFile, 500)
	if !ok {
		t.Fatal("BuildCountUnmarkedOrphanNodesQuery() ok = false, want true")
	}
	for _, want := range []string{
		"MATCH (n:File)",
		"n.eshu_orphan_observed_at_unix IS NULL",
		"NOT (n)--()",
		"RETURN count(n) AS orphan_count",
	} {
		if !strings.Contains(unmarked.Cypher, want) {
			t.Fatalf("unmarked-count Cypher missing %q:\n%s", want, unmarked.Cypher)
		}
	}

	relinked, ok := BuildCountMarkedRelinkedNodesQuery(OrphanSweepLabelFile, 500)
	if !ok {
		t.Fatal("BuildCountMarkedRelinkedNodesQuery() ok = false, want true")
	}
	if !strings.Contains(relinked.Cypher, "n.eshu_orphan_observed_at_unix IS NOT NULL") {
		t.Fatalf("marked-relinked Cypher missing marker predicate:\n%s", relinked.Cypher)
	}
	if !strings.Contains(relinked.Cypher, "AND (n)--()") {
		t.Fatalf("marked-relinked Cypher must require a relationship:\n%s", relinked.Cypher)
	}
	if strings.Contains(relinked.Cypher, "NOT (n)--()") {
		t.Fatalf("marked-relinked Cypher must not require zero relationships:\n%s", relinked.Cypher)
	}
}
