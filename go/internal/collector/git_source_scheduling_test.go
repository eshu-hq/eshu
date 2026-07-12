// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeRepoWithFiles creates a temp directory populated with n regular files and
// returns its absolute path.
func makeRepoWithFiles(t *testing.T, n int) string {
	t.Helper()

	dir := t.TempDir()
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file_%04d.py", i))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}
	return dir
}

func TestResolveRepositoriesSortsLargestFirst(t *testing.T) {
	t.Parallel()

	// Three repos with distinct file counts, supplied in ascending order so the
	// sort must reorder them.
	small := makeRepoWithFiles(t, 2)
	medium := makeRepoWithFiles(t, 10)
	large := makeRepoWithFiles(t, 40)

	batch := SelectionBatch{
		ObservedAt: time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC),
		Repositories: []SelectedRepository{
			{RepoPath: small},
			{RepoPath: medium},
			{RepoPath: large},
		},
	}

	source := &GitSource{}
	resolved, _, _, err := source.resolveRepositories(batch)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v, want nil", err)
	}

	gotOrder := make([]string, 0, len(resolved))
	for _, repo := range resolved {
		gotOrder = append(gotOrder, repo.RepoPath)
	}
	wantOrder := []string{large, medium, small}
	for i := range wantOrder {
		want, err := filepath.Abs(wantOrder[i])
		if err != nil {
			t.Fatalf("filepath.Abs(%q) error = %v", wantOrder[i], err)
		}
		if gotOrder[i] != want {
			t.Fatalf("resolved[%d] = %q, want %q (largest-first)\nfull order: %#v", i, gotOrder[i], want, gotOrder)
		}
	}

	// The exact set of repos must be preserved (sort only reorders, never drops
	// or duplicates).
	if len(resolved) != len(batch.Repositories) {
		t.Fatalf("resolved length = %d, want %d", len(resolved), len(batch.Repositories))
	}
}

func TestResolveRepositoriesStableForEqualCounts(t *testing.T) {
	t.Parallel()

	// Two repos with the same file count must keep their input relative order
	// (stable sort) so scheduling is deterministic.
	first := makeRepoWithFiles(t, 5)
	second := makeRepoWithFiles(t, 5)

	batch := SelectionBatch{
		ObservedAt: time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC),
		Repositories: []SelectedRepository{
			{RepoPath: first},
			{RepoPath: second},
		},
	}

	source := &GitSource{}
	resolved, _, _, err := source.resolveRepositories(batch)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v, want nil", err)
	}

	wantFirst, _ := filepath.Abs(first)
	wantSecond, _ := filepath.Abs(second)
	if resolved[0].RepoPath != wantFirst || resolved[1].RepoPath != wantSecond {
		t.Fatalf("equal-count order not stable: got [%q, %q], want [%q, %q]",
			resolved[0].RepoPath, resolved[1].RepoPath, wantFirst, wantSecond)
	}
}

func TestResolveRepositoriesCountsAlignWithResolvedOrder(t *testing.T) {
	t.Parallel()

	// counts[i] must be the file count of resolved[i] after the largest-first
	// sort, because startStream classifies the small/large lanes by index.
	small := makeRepoWithFiles(t, 3)
	medium := makeRepoWithFiles(t, 7)
	large := makeRepoWithFiles(t, 25)

	batch := SelectionBatch{
		ObservedAt: time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC),
		Repositories: []SelectedRepository{
			{RepoPath: medium},
			{RepoPath: large},
			{RepoPath: small},
		},
	}

	source := &GitSource{}
	resolved, counts, _, err := source.resolveRepositories(batch)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v, want nil", err)
	}
	if len(counts) != len(resolved) {
		t.Fatalf("counts length = %d, want %d (must align 1:1 with resolved)", len(counts), len(resolved))
	}
	wantCounts := []int{25, 7, 3}
	for i, want := range wantCounts {
		if counts[i] != want {
			t.Fatalf("counts[%d] = %d, want %d (aligned with resolved order)\ncounts: %v", i, counts[i], want, counts)
		}
		if got := countRepositoryFiles(resolved[i].RepoPath); got != counts[i] {
			t.Fatalf("counts[%d] = %d but resolved[%d] has %d files (misaligned)", i, counts[i], i, got)
		}
	}
}

func TestResolveRepositoriesLargeBatchDeterministicOrder(t *testing.T) {
	t.Parallel()

	// A batch larger than any internal worker pool, with tie blocks, must
	// produce a deterministic largest-first order with equal counts kept in
	// input order — identically across repeated calls.
	const batchSize = 30
	repos := make([]SelectedRepository, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		// File counts cycle through {4, 4, 9}: many equal-count ties plus
		// interleaved larger repos so the sort must interleave and the tie
		// blocks must preserve input order.
		n := 4
		if i%3 == 2 {
			n = 9
		}
		repos = append(repos, SelectedRepository{RepoPath: makeRepoWithFiles(t, n)})
	}

	batch := SelectionBatch{
		ObservedAt:   time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC),
		Repositories: repos,
	}

	source := &GitSource{}
	firstResolved, firstCounts, _, err := source.resolveRepositories(batch)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v, want nil", err)
	}

	// Largest-first: all 9-count repos precede all 4-count repos, and within
	// each tie block input order is preserved.
	wantNine := batchSize / 3
	for i, count := range firstCounts {
		if i < wantNine && count != 9 {
			t.Fatalf("counts[%d] = %d, want 9 (largest block first)\ncounts: %v", i, count, firstCounts)
		}
		if i >= wantNine && count != 4 {
			t.Fatalf("counts[%d] = %d, want 4 (tie block after)\ncounts: %v", i, count, firstCounts)
		}
	}
	wantOrder := make([]string, 0, batchSize)
	for i, r := range repos {
		if i%3 == 2 {
			wantOrder = append(wantOrder, r.RepoPath)
		}
	}
	for i, r := range repos {
		if i%3 != 2 {
			wantOrder = append(wantOrder, r.RepoPath)
		}
	}
	for i := range wantOrder {
		wantAbs, err := filepath.Abs(wantOrder[i])
		if err != nil {
			t.Fatalf("filepath.Abs(%q) error = %v", wantOrder[i], err)
		}
		if firstResolved[i].RepoPath != wantAbs {
			t.Fatalf("resolved[%d] = %q, want %q (tie blocks must keep input order)", i, firstResolved[i].RepoPath, wantAbs)
		}
	}

	// Repeat call must produce the identical order and counts.
	secondResolved, secondCounts, _, err := source.resolveRepositories(batch)
	if err != nil {
		t.Fatalf("resolveRepositories() second call error = %v, want nil", err)
	}
	for i := range firstResolved {
		if firstResolved[i].RepoPath != secondResolved[i].RepoPath || firstCounts[i] != secondCounts[i] {
			t.Fatalf("run-to-run mismatch at %d: (%q,%d) vs (%q,%d)",
				i, firstResolved[i].RepoPath, firstCounts[i], secondResolved[i].RepoPath, secondCounts[i])
		}
	}
}

func TestResolveRepositoriesEmptyBatch(t *testing.T) {
	t.Parallel()

	source := &GitSource{}
	resolved, counts, sourceRunID, err := source.resolveRepositories(SelectionBatch{
		ObservedAt: time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v, want nil", err)
	}
	if len(resolved) != 0 || len(counts) != 0 {
		t.Fatalf("empty batch: resolved=%d counts=%d, want 0/0", len(resolved), len(counts))
	}
	if sourceRunID == "" {
		t.Fatal("empty batch: sourceRunID empty, want stable ID")
	}
}

func TestIsLargeRepositoryReturnsExactCountBelowThreshold(t *testing.T) {
	t.Parallel()

	dir := makeRepoWithFiles(t, 37)

	large, count := isLargeRepository(dir, 500)
	if large {
		t.Fatal("isLargeRepository large = true, want false for 37 files at threshold 500")
	}
	if count != 37 {
		t.Fatalf("isLargeRepository count = %d, want 37", count)
	}
}

func TestIsLargeRepositoryReturnsExactCountAboveThreshold(t *testing.T) {
	t.Parallel()

	dir := makeRepoWithFiles(t, 120)

	large, count := isLargeRepository(dir, 50)
	if !large {
		t.Fatal("isLargeRepository large = false, want true for 120 files at threshold 50")
	}
	if count != 120 {
		t.Fatalf("isLargeRepository count = %d, want 120 (full count, no early bail)", count)
	}
}
