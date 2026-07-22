// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// gitTrackedFilesBenchFileCount matches the local 906-repo benchmark corpus's
// median tracked-file count (issue #5591 lazy-resolution perf evidence; see
// evidence-5591-tracked-ignored-perf.md), not an invented round number.
const gitTrackedFilesBenchFileCount = 36

// benchGitTrackedFilesFixture creates a small real git repository, once,
// outside any benchmark timing loop, with gitTrackedFilesBenchFileCount
// committed files — the single `git ls-files -z` subprocess
// BenchmarkGitTrackedFiles measures is what buildGitTrackedResolver's lazy
// resolveTracked() closure spawns at most once per repo root per snapshot
// (issue #5591).
func benchGitTrackedFilesFixture(b *testing.B) string {
	b.Helper()
	repoPath := b.TempDir()
	benchRunGit(b, repoPath, "init", "-b", "main")
	benchRunGit(b, repoPath, "config", "user.email", "bench@example.com")
	benchRunGit(b, repoPath, "config", "user.name", "Bench")
	for i := 0; i < gitTrackedFilesBenchFileCount; i++ {
		name := "file" + strconv.Itoa(i) + ".go"
		if err := os.WriteFile(filepath.Join(repoPath, name), []byte("package bench\n"), 0o600); err != nil {
			b.Fatalf("write %s: %v", name, err)
		}
	}
	benchRunGit(b, repoPath, "add", ".")
	benchRunGit(b, repoPath, "commit", "-m", "initial")
	return repoPath
}

// BenchmarkGitTrackedFiles isolates the single `git ls-files -z` subprocess
// buildGitTrackedResolver's lazy resolveTracked() closure spawns (issue
// #5591 lazy-resolution perf evidence; see
// evidence-5591-tracked-ignored-perf.md). It is the ONLY per-repo-root cost
// this fix adds, and only when a discovered file actually matches
// .gitignore — a repo with zero ignore-matched candidates pays none of it.
func BenchmarkGitTrackedFiles(b *testing.B) {
	repoPath := benchGitTrackedFilesFixture(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := gitTrackedFiles(ctx, repoPath); !ok {
			b.Fatal("gitTrackedFiles() ok = false, want true")
		}
	}
}
