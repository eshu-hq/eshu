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
