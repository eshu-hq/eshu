// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestBuildParseSubtreePartitionsSplitsStableSubtrees(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	files := []string{
		filepath.Join(repoRoot, "README.md"),
		filepath.Join(repoRoot, "services", "api", "app.py"),
		filepath.Join(repoRoot, "services", "api", "handlers.py"),
		filepath.Join(repoRoot, "services", "api", "models.py"),
		filepath.Join(repoRoot, "services", "worker", "main.py"),
		filepath.Join(repoRoot, "web", "src", "app.py"),
	}

	partitions := buildParseSubtreePartitions(repoRoot, files, 3)

	gotKeys := make([]string, 0, len(partitions))
	gotIndexes := make([][]int, 0, len(partitions))
	for _, partition := range partitions {
		gotKeys = append(gotKeys, partition.key)
		indexes := make([]int, 0, len(partition.jobs))
		for _, job := range partition.jobs {
			indexes = append(indexes, job.index)
		}
		gotIndexes = append(gotIndexes, indexes)
	}
	wantKeys := []string{".", "services/api#001", "services/api#002", "services/worker", "web/src"}
	wantIndexes := [][]int{{0}, {1, 2}, {3}, {4}, {5}}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("partition keys = %#v, want %#v", gotKeys, wantKeys)
	}
	if !reflect.DeepEqual(gotIndexes, wantIndexes) {
		t.Fatalf("partition indexes = %#v, want %#v", gotIndexes, wantIndexes)
	}
}

func TestPartitionedConcurrentParseMatchesSequentialComposition(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	files := []string{
		filepath.Join(repoRoot, "services", "api", "app.py"),
		filepath.Join(repoRoot, "services", "api", "handlers.py"),
		filepath.Join(repoRoot, "services", "worker", "main.py"),
		filepath.Join(repoRoot, "web", "src", "view.py"),
	}
	for _, file := range files {
		writeCollectorTestFile(t, file, "def handler():\n    return 1\n")
	}
	fileSet := discovery.RepoFileSet{Files: files}
	engine := defaultCollectorTestEngine(t)

	sequential := NativeRepositorySnapshotter{ParseWorkers: 1}
	seqShape, seqParsed, _, err := sequential.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		fileSet,
		engine,
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("sequential buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	concurrent := NativeRepositorySnapshotter{ParseWorkers: 3}
	gotShape, gotParsed, _, err := concurrent.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		fileSet,
		engine,
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("concurrent buildParsedRepositoryFiles() error = %v, want nil", err)
	}

	if !reflect.DeepEqual(gotShape, seqShape) {
		t.Fatalf("concurrent shape files differ from sequential\n got: %#v\nwant: %#v", gotShape, seqShape)
	}
	if !reflect.DeepEqual(gotParsed, seqParsed) {
		t.Fatalf("concurrent parsed files differ from sequential\n got: %#v\nwant: %#v", gotParsed, seqParsed)
	}
}

func BenchmarkPartitionedParseLargeMonorepo(b *testing.B) {
	for _, workers := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			repoRoot := b.TempDir()
			files := make([]string, 0, 96)
			for service := 0; service < 12; service++ {
				for fileIndex := 0; fileIndex < 8; fileIndex++ {
					path := filepath.Join(
						repoRoot,
						"services",
						fmt.Sprintf("svc-%02d", service),
						fmt.Sprintf("module_%02d.py", fileIndex),
					)
					writeCollectorBenchmarkFile(b, path, fmt.Sprintf(
						"def handler_%02d_%02d():\n    return %d\n",
						service,
						fileIndex,
						service+fileIndex,
					))
					files = append(files, path)
				}
			}
			engine := benchmarkCollectorEngine(b)
			snapshotter := NativeRepositorySnapshotter{ParseWorkers: workers}
			fileSet := discovery.RepoFileSet{Files: files}
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				shapeFiles, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
					context.Background(),
					repoRoot,
					fileSet,
					engine,
					"commit-sha",
					false,
					parser.GoPackageSemanticRoots{},
					"repo-alpha",
				)
				if err != nil {
					b.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
				}
				if len(shapeFiles) != len(files) || len(parsedFiles) != len(files) {
					b.Fatalf("parsed %d/%d files, want %d/%d", len(shapeFiles), len(parsedFiles), len(files), len(files))
				}
			}
		})
	}
}

func benchmarkCollectorEngine(b *testing.B) *parser.Engine {
	b.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	return engine
}

func writeCollectorBenchmarkFile(b *testing.B, path string, body string) {
	b.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		b.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}
