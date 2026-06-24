// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

const deltaSnapshotBenchmarkFileCount = 400

func BenchmarkNativeRepositorySnapshotterFullFixture(b *testing.B) {
	repoRoot, _ := buildDeltaSnapshotBenchmarkFixture(b, deltaSnapshotBenchmarkFileCount)
	snapshotter := benchmarkNativeRepositorySnapshotter(b)
	ctx := context.Background()

	b.ReportMetric(deltaSnapshotBenchmarkFileCount, "fixture_files")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapshot, err := snapshotter.SnapshotRepository(ctx, SelectedRepository{RepoPath: repoRoot})
		if err != nil {
			b.Fatalf("SnapshotRepository() error = %v, want nil", err)
		}
		if snapshot.FileCount != deltaSnapshotBenchmarkFileCount {
			b.Fatalf("FileCount = %d, want %d", snapshot.FileCount, deltaSnapshotBenchmarkFileCount)
		}
	}
}

func BenchmarkNativeRepositorySnapshotterDeltaSingleFileFixture(b *testing.B) {
	repoRoot, changedFile := buildDeltaSnapshotBenchmarkFixture(b, deltaSnapshotBenchmarkFileCount)
	snapshotter := benchmarkNativeRepositorySnapshotter(b)
	ctx := context.Background()

	b.ReportMetric(deltaSnapshotBenchmarkFileCount, "fixture_files")
	b.ReportMetric(1, "changed_files")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapshot, err := snapshotter.SnapshotRepository(ctx, SelectedRepository{
			RepoPath:     repoRoot,
			FileTargets:  []string{changedFile},
			Delta:        true,
			IsDependency: false,
		})
		if err != nil {
			b.Fatalf("SnapshotRepository() error = %v, want nil", err)
		}
		if snapshot.FileCount != 1 {
			b.Fatalf("FileCount = %d, want 1", snapshot.FileCount)
		}
	}
}

func buildDeltaSnapshotBenchmarkFixture(b *testing.B, fileCount int) (string, string) {
	b.Helper()

	repoRoot := b.TempDir()
	sourceDir := filepath.Join(repoRoot, "pkg", "bench")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		b.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	var changedFile string
	for i := 0; i < fileCount; i++ {
		filePath := filepath.Join(sourceDir, fmt.Sprintf("file_%03d.go", i))
		source := fmt.Sprintf("package bench\n\nfunc Function%03d() int {\n\treturn %d\n}\n", i, i)
		if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
			b.Fatalf("WriteFile() error = %v, want nil", err)
		}
		if i == fileCount/2 {
			changedFile = filePath
		}
	}
	return repoRoot, changedFile
}

func benchmarkNativeRepositorySnapshotter(b *testing.B) NativeRepositorySnapshotter {
	b.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	return NativeRepositorySnapshotter{
		Engine: engine,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}
