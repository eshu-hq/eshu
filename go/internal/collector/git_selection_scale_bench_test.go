// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestRepositoryShardScaleSummaryCoversEveryRepositoryOnce(t *testing.T) {
	t.Parallel()

	summary := repositoryShardScaleSummary(10_000, 8)
	if got, want := summary.totalSelected, 10_000; got != want {
		t.Fatalf("total selected = %d, want %d", got, want)
	}
	if got, want := summary.duplicateSelections, 0; got != want {
		t.Fatalf("duplicate selections = %d, want %d", got, want)
	}
	if got, want := summary.missingSelections, 0; got != want {
		t.Fatalf("missing selections = %d, want %d", got, want)
	}
	if spread := summary.maxShardSize - summary.minShardSize; spread > 100 {
		t.Fatalf("shard size spread = %d, want <= 100; counts=%v", spread, summary.shardSizes)
	}
}

func TestRepositoryShardParseThroughputSummaryParsesEveryRepositoryOnce(t *testing.T) {
	t.Parallel()

	fixture := newRepositoryShardParseFixture(t, 64)
	summary := repositoryShardParseThroughputSummary(t, fixture, repositoryShardParseEngine(t), 4, 2)
	if got, want := summary.totalSelected, 64; got != want {
		t.Fatalf("total selected = %d, want %d", got, want)
	}
	if got, want := summary.totalParsedFiles, 128; got != want {
		t.Fatalf("total parsed files = %d, want %d", got, want)
	}
	if got, want := summary.duplicateSelections, 0; got != want {
		t.Fatalf("duplicate selections = %d, want %d", got, want)
	}
	if got, want := summary.missingSelections, 0; got != want {
		t.Fatalf("missing selections = %d, want %d", got, want)
	}
}

func BenchmarkRepositoryShardSelectionScale(b *testing.B) {
	for _, repoCount := range []int{1_000, 10_000} {
		for _, shardCount := range []int{1, 4, 8} {
			b.Run(fmt.Sprintf("repos_%d/shards_%d", repoCount, shardCount), func(b *testing.B) {
				b.ReportAllocs()

				var summary repositoryShardScaleReport
				for i := 0; i < b.N; i++ {
					summary = repositoryShardScaleSummary(repoCount, shardCount)
				}
				if summary.totalSelected != repoCount || summary.duplicateSelections != 0 || summary.missingSelections != 0 {
					b.Fatalf("summary = %#v, want complete single coverage for %d repos", summary, repoCount)
				}
				b.ReportMetric(float64(summary.maxShardSize-summary.minShardSize), "repos_spread")
			})
		}
	}
}

func BenchmarkRepositoryShardParseThroughput(b *testing.B) {
	for _, shardCount := range []int{1, 4, 8} {
		b.Run(fmt.Sprintf("repos_2000/shards_%d", shardCount), func(b *testing.B) {
			fixture := newRepositoryShardParseFixture(b, 2_000)
			engine := repositoryShardParseEngine(b)
			b.ReportAllocs()
			b.ResetTimer()

			var summary repositoryShardParseReport
			for i := 0; i < b.N; i++ {
				summary = repositoryShardParseThroughputSummary(b, fixture, engine, shardCount, 2)
			}
			if summary.totalSelected != len(fixture.repositoryIDs) || summary.duplicateSelections != 0 || summary.missingSelections != 0 {
				b.Fatalf("summary = %#v, want complete single parse coverage for %d repos", summary, len(fixture.repositoryIDs))
			}
			b.ReportMetric(float64(summary.maxShardSize-summary.minShardSize), "repos_spread")
			b.ReportMetric(float64(summary.totalParsedFiles), "parsed_files")
			b.ReportMetric(summary.maxShardDuration.Seconds(), "max_shard_seconds")
			if summary.maxShardDuration > 0 {
				b.ReportMetric(float64(summary.totalSelected)/summary.maxShardDuration.Seconds(), "repos_per_parallel_second")
			}
		})
	}
}

type repositoryShardScaleReport struct {
	shardSizes          []int
	totalSelected       int
	duplicateSelections int
	missingSelections   int
	minShardSize        int
	maxShardSize        int
}

type repositoryShardParseFixture struct {
	repositoryIDs []string
	repoPaths     map[string]string
	filesByID     map[string][]string
}

type repositoryShardParseReport struct {
	shardSizes          []int
	shardDurations      []time.Duration
	totalSelected       int
	totalParsedFiles    int
	duplicateSelections int
	missingSelections   int
	minShardSize        int
	maxShardSize        int
	maxShardDuration    time.Duration
}

func repositoryShardScaleSummary(repoCount int, shardCount int) repositoryShardScaleReport {
	repositoryIDs := syntheticRepositoryIDs(repoCount)
	seen := make(map[string]int, repoCount)
	report := repositoryShardScaleReport{
		shardSizes:   make([]int, 0, shardCount),
		minShardSize: math.MaxInt,
	}

	for shardIndex := 0; shardIndex < shardCount; shardIndex++ {
		selected := filterRepositoryIDsByShard(repositoryIDs, RepoSyncConfig{
			RepoShardCount: shardCount,
			RepoShardIndex: shardIndex,
		})
		report.shardSizes = append(report.shardSizes, len(selected))
		report.totalSelected += len(selected)
		if len(selected) < report.minShardSize {
			report.minShardSize = len(selected)
		}
		if len(selected) > report.maxShardSize {
			report.maxShardSize = len(selected)
		}
		for _, repositoryID := range selected {
			seen[repositoryID]++
		}
	}

	if shardCount == 0 {
		report.minShardSize = 0
	}
	for _, count := range seen {
		if count > 1 {
			report.duplicateSelections += count - 1
		}
	}
	report.missingSelections = repoCount - len(seen)
	return report
}

func repositoryShardParseThroughputSummary(
	tb testing.TB,
	fixture repositoryShardParseFixture,
	engine *parser.Engine,
	shardCount int,
	parseWorkers int,
) repositoryShardParseReport {
	tb.Helper()

	seen := make(map[string]int, len(fixture.repositoryIDs))
	report := repositoryShardParseReport{
		shardSizes:     make([]int, 0, shardCount),
		shardDurations: make([]time.Duration, 0, shardCount),
		minShardSize:   math.MaxInt,
	}
	snapshotter := NativeRepositorySnapshotter{ParseWorkers: parseWorkers}

	for shardIndex := 0; shardIndex < shardCount; shardIndex++ {
		selected := filterRepositoryIDsByShard(fixture.repositoryIDs, RepoSyncConfig{
			RepoShardCount: shardCount,
			RepoShardIndex: shardIndex,
		})
		startedAt := time.Now()
		for _, repositoryID := range selected {
			files := fixture.filesByID[repositoryID]
			shapeFiles, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
				context.Background(),
				fixture.repoPaths[repositoryID],
				discovery.RepoFileSet{Files: files},
				engine,
				"commit-sha",
				false,
				parser.GoPackageSemanticRoots{},
				repositoryID,
			)
			if err != nil {
				tb.Fatalf("buildParsedRepositoryFiles(%q) error = %v, want nil", repositoryID, err)
			}
			if len(shapeFiles) != len(files) || len(parsedFiles) != len(files) {
				tb.Fatalf("repository %q parsed %d/%d files, want %d/%d", repositoryID, len(shapeFiles), len(parsedFiles), len(files), len(files))
			}
			report.totalParsedFiles += len(parsedFiles)
			seen[repositoryID]++
		}
		elapsed := time.Since(startedAt)
		report.shardDurations = append(report.shardDurations, elapsed)
		report.shardSizes = append(report.shardSizes, len(selected))
		report.totalSelected += len(selected)
		if len(selected) < report.minShardSize {
			report.minShardSize = len(selected)
		}
		if len(selected) > report.maxShardSize {
			report.maxShardSize = len(selected)
		}
		if elapsed > report.maxShardDuration {
			report.maxShardDuration = elapsed
		}
	}

	if shardCount == 0 {
		report.minShardSize = 0
	}
	for _, count := range seen {
		if count > 1 {
			report.duplicateSelections += count - 1
		}
	}
	report.missingSelections = len(fixture.repositoryIDs) - len(seen)
	return report
}

func newRepositoryShardParseFixture(tb testing.TB, repoCount int) repositoryShardParseFixture {
	tb.Helper()

	root := tb.TempDir()
	ids := syntheticRepositoryIDs(repoCount)
	fixture := repositoryShardParseFixture{
		repositoryIDs: ids,
		repoPaths:     make(map[string]string, repoCount),
		filesByID:     make(map[string][]string, repoCount),
	}
	for index, repositoryID := range ids {
		repoPath := filepath.Join(root, fmt.Sprintf("repo-%05d", index))
		appPath := filepath.Join(repoPath, "app.py")
		helperPath := filepath.Join(repoPath, "helpers.py")
		writeRepositoryShardParseFile(tb, appPath, fmt.Sprintf(
			"from helpers import helper_%05d\n\ndef handler_%05d():\n    return helper_%05d()\n",
			index,
			index,
			index,
		))
		writeRepositoryShardParseFile(tb, helperPath, fmt.Sprintf(
			"def helper_%05d():\n    return %d\n",
			index,
			index,
		))
		fixture.repoPaths[repositoryID] = repoPath
		fixture.filesByID[repositoryID] = []string{appPath, helperPath}
	}
	return fixture
}

func syntheticRepositoryIDs(repoCount int) []string {
	repositoryIDs := make([]string, 0, repoCount)
	for i := 0; i < repoCount; i++ {
		repositoryIDs = append(repositoryIDs, fmt.Sprintf("github.com/example/repo-%05d", i))
	}
	return repositoryIDs
}

func writeRepositoryShardParseFile(tb testing.TB, path string, body string) {
	tb.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		tb.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		tb.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}

func repositoryShardParseEngine(tb testing.TB) *parser.Engine {
	tb.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		tb.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	return engine
}
