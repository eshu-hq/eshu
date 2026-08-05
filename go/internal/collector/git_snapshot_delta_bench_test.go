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
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
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

func BenchmarkNativeRepositorySnapshotterDeltaSingleFileWithWorkflowSnapshot(b *testing.B) {
	for _, workflowCount := range []int{1, 5, 10, 100} {
		b.Run(fmt.Sprintf("workflows_%03d", workflowCount), func(b *testing.B) {
			repoRoot, changedFile := buildDeltaSnapshotBenchmarkFixture(b, deltaSnapshotBenchmarkFileCount)
			addDeltaWorkflowBenchmarkFixture(b, repoRoot, workflowCount)
			snapshotter := benchmarkNativeRepositorySnapshotter(b)
			ctx := context.Background()

			b.ReportMetric(deltaSnapshotBenchmarkFileCount, "source_files")
			b.ReportMetric(float64(workflowCount), "workflow_files")
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
				if got, want := snapshot.FileCount, 1; got != want {
					b.Fatalf("FileCount = %d, want %d", got, want)
				}
				if got, want := len(snapshot.WorkflowImageFileMetas), workflowCount; got != want {
					b.Fatalf("len(WorkflowImageFileMetas) = %d, want %d", got, want)
				}
			}
		})
	}
}

func BenchmarkNativeRepositoryDeltaGenerationWithWorkflowSnapshot(b *testing.B) {
	for _, workflowCount := range []int{0, 1, 5, 10, 100} {
		b.Run(fmt.Sprintf("workflows_%03d", workflowCount), func(b *testing.B) {
			repoRoot, changedFile := buildDeltaSnapshotBenchmarkFixture(b, deltaSnapshotBenchmarkFileCount)
			addDeltaWorkflowBenchmarkFixture(b, repoRoot, workflowCount)
			snapshotter := benchmarkNativeRepositorySnapshotter(b)
			ctx := context.Background()
			repo := repositoryidentity.Metadata{ID: "repository:workflow-benchmark", Name: "workflow-benchmark"}

			b.ReportMetric(deltaSnapshotBenchmarkFileCount, "source_files")
			b.ReportMetric(float64(workflowCount), "workflow_files")
			b.ReportMetric(1, "changed_files")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				snapshot, err := snapshotter.SnapshotRepository(ctx, SelectedRepository{
					RepoPath:        repoRoot,
					FileTargets:     []string{changedFile},
					Delta:           true,
					SourceCommitSHA: "workflow-benchmark-commit",
				})
				if err != nil {
					b.Fatalf("SnapshotRepository() error = %v, want nil", err)
				}
				collected := buildStreamingGeneration(
					repoRoot,
					repo,
					"workflow-benchmark-run",
					time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
					snapshot,
					false,
					"",
				)
				for range collected.Facts {
				}
			}
		})
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

func addDeltaWorkflowBenchmarkFixture(b *testing.B, repoRoot string, workflowCount int) {
	b.Helper()
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		b.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	for i := 0; i < workflowCount; i++ {
		filePath := filepath.Join(workflowDir, fmt.Sprintf("build_%03d.yml", i))
		body := workflowImageDeltaFixture(fmt.Sprintf("ghcr.io/acme/service-%03d:v1", i))
		if err := os.WriteFile(filePath, []byte(body), 0o644); err != nil {
			b.Fatalf("WriteFile() error = %v, want nil", err)
		}
	}
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
