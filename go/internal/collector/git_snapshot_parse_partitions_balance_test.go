// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// collectPartitionJobs flattens every partition's jobs into a single slice,
// preserving discovery order within partitions.
func collectPartitionJobs(partitions []parseSubtreePartition) []parseFileJob {
	jobs := make([]parseFileJob, 0)
	for _, partition := range partitions {
		jobs = append(jobs, partition.jobs...)
	}
	return jobs
}

// partitionWeight sums the estimated parse weight production scheduling uses.
func partitionWeight(partition parseSubtreePartition) int64 {
	var total int64
	for _, job := range partition.jobs {
		total += parseFileWeightBytes(job.path)
	}
	return total
}

func writeSizedFile(t *testing.T, path string, size int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// TestBuildParseSubtreePartitionsCoversExactFileSet proves the partition
// planner is loss-free: the union of all partitions' jobs equals the input
// files exactly — same set, same indexes, no drop, no duplicate — for a mix of
// file sizes. This is the correctness invariant: the parse result must be
// identical to before, only the worker distribution changes.
func TestBuildParseSubtreePartitionsCoversExactFileSet(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	// Mixed sizes across several subtrees, including a couple of heavy files.
	spec := []struct {
		rel  string
		size int
	}{
		{"README.md", 100},
		{filepath.Join("services", "api", "app.py"), 200},
		{filepath.Join("services", "api", "handlers.py"), 50_000},
		{filepath.Join("services", "api", "models.py"), 300},
		{filepath.Join("services", "api", "util.py"), 80_000},
		{filepath.Join("services", "worker", "main.py"), 400},
		{filepath.Join("web", "src", "view.py"), 250},
		{filepath.Join("web", "src", "big.py"), 120_000},
	}
	files := make([]string, 0, len(spec))
	for _, s := range spec {
		path := filepath.Join(repoRoot, s.rel)
		writeSizedFile(t, path, s.size)
		files = append(files, path)
	}

	partitions := buildParseSubtreePartitions(repoRoot, files, 3)

	got := collectPartitionJobs(partitions)
	if len(got) != len(files) {
		t.Fatalf("partition job count = %d, want %d (no drop/duplicate)", len(got), len(files))
	}

	seen := make(map[int]parseFileJob, len(got))
	for _, job := range got {
		if existing, dup := seen[job.index]; dup {
			t.Fatalf("index %d appears twice: %q and %q", job.index, existing.path, job.path)
		}
		seen[job.index] = job
	}
	for index, path := range files {
		job, ok := seen[index]
		if !ok {
			t.Fatalf("index %d (%q) missing from partitions", index, path)
		}
		if job.path != path {
			t.Fatalf("index %d path = %q, want %q (index→path must be preserved)", index, job.path, path)
		}
	}
}

// TestBuildParseSubtreePartitionsSpreadsHeavyFiles proves a few huge files are
// spread across partitions rather than clustering in one. The heaviest
// partition's parse weight must stay within a bounded factor of the per-worker
// target (total/workers), so no single worker carries all the heavy files.
func TestBuildParseSubtreePartitionsSpreadsHeavyFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	// One subtree dominated by several heavy files plus light noise elsewhere.
	files := make([]string, 0, 16)
	for i := 0; i < 6; i++ {
		path := filepath.Join(repoRoot, "heavy", fmt.Sprintf("blob_%02d.py", i))
		writeSizedFile(t, path, 100_000)
		files = append(files, path)
	}
	for i := 0; i < 10; i++ {
		path := filepath.Join(repoRoot, "light", fmt.Sprintf("small_%02d.py", i))
		writeSizedFile(t, path, 100)
		files = append(files, path)
	}

	workers := 4
	partitions := buildParseSubtreePartitions(repoRoot, files, workers)

	var totalWeight int64
	var maxPartitionWeight int64
	for _, partition := range partitions {
		weight := partitionWeight(partition)
		totalWeight += weight
		if weight > maxPartitionWeight {
			maxPartitionWeight = weight
		}
	}

	target := totalWeight / int64(workers)
	// The heaviest partition must not exceed the per-worker target by more than
	// one extra heavy file (100_000 weight). A count-only balancer would let one
	// partition hold 6 heavy files (~600_000 weight); cost-aware chunking caps it.
	bound := target + 100_000
	if maxPartitionWeight > bound {
		t.Fatalf("max partition weight = %d exceeds bound %d (target %d); heavy files clustered",
			maxPartitionWeight, bound, target)
	}
}

// TestBuildParseSubtreePartitionsSpreadsEmptyFiles proves the per-file weight
// floor spreads a large group of empty files across workers by count. Without
// the floor each empty file would weigh ~0 bytes, so the whole group would
// collapse into a single partition (re-creating the count-clustering that
// byte-balancing avoids); with the floor each empty file weighs
// minParseFileWeightBytes, so the per-worker byte target splits them.
func TestBuildParseSubtreePartitionsSpreadsEmptyFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	const fileCount = 64
	files := make([]string, 0, fileCount)
	for i := 0; i < fileCount; i++ {
		path := filepath.Join(repoRoot, "empties", fmt.Sprintf("mod_%02d.py", i))
		writeSizedFile(t, path, 0)
		files = append(files, path)
	}

	partitions := buildParseSubtreePartitions(repoRoot, files, 4)

	if len(partitions) < 2 {
		t.Fatalf("empty files collapsed into %d partition(s); want them spread across workers", len(partitions))
	}
	maxJobs := 0
	for _, partition := range partitions {
		if n := len(partition.jobs); n > maxJobs {
			maxJobs = n
		}
	}
	if maxJobs >= fileCount {
		t.Fatalf("max partition holds %d/%d files; empty files clustered into one partition", maxJobs, fileCount)
	}
}

// TestBuildParseSubtreePartitionsSpreadsTinyExpensiveLanguageFiles proves tiny
// files for slower parser adapters are weighted high enough to spread across
// parse workers even when a large cheap file in the same subtree dominates raw
// bytes. This protects the remote full-corpus path where PHP/JS/TS/Python parse
// cost is not proportional to file size.
func TestBuildParseSubtreePartitionsSpreadsTinyExpensiveLanguageFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	files := make([]string, 0, 41)
	for i := 0; i < 40; i++ {
		path := filepath.Join(repoRoot, "app", "src", fmt.Sprintf("handler_%02d.php", i))
		writeSizedFile(t, path, 64)
		files = append(files, path)
	}
	largeConfig := filepath.Join(repoRoot, "app", "src", "generated.tf")
	writeSizedFile(t, largeConfig, 1<<20)
	files = append(files, largeConfig)

	partitions := buildParseSubtreePartitions(repoRoot, files, 4)

	maxPHPJobs := 0
	for _, partition := range partitions {
		phpJobs := 0
		for _, job := range partition.jobs {
			if strings.HasSuffix(job.path, ".php") {
				phpJobs++
			}
		}
		if phpJobs > maxPHPJobs {
			maxPHPJobs = phpJobs
		}
	}
	if maxPHPJobs > 20 {
		t.Fatalf("max PHP jobs per partition = %d, want <= 20; tiny expensive parser files clustered", maxPHPJobs)
	}
}

func TestParseFileWeightUsesCostlyParserAliases(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	for _, ext := range []string{".cjs", ".mjs", ".cts", ".mts", ".pyw", ".ipynb"} {
		ext := ext
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(repoRoot, "sample"+ext)
			writeSizedFile(t, path, 64)
			if got := parseFileWeightBytes(path); got < minCostlyParserFileWeightBytes {
				t.Fatalf("parseFileWeightBytes(%q) = %d, want at least %d", ext, got, minCostlyParserFileWeightBytes)
			}
		})
	}
}

// TestBuildParseSubtreePartitionsEdgeCases covers empty, single-file, and
// all-same-size inputs.
func TestBuildParseSubtreePartitionsEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if got := buildParseSubtreePartitions(t.TempDir(), nil, 4); got != nil {
			t.Fatalf("empty input partitions = %#v, want nil", got)
		}
	})

	t.Run("single_file", func(t *testing.T) {
		t.Parallel()
		repoRoot := t.TempDir()
		path := filepath.Join(repoRoot, "only.py")
		writeSizedFile(t, path, 1234)
		partitions := buildParseSubtreePartitions(repoRoot, []string{path}, 4)
		jobs := collectPartitionJobs(partitions)
		if len(jobs) != 1 || jobs[0].index != 0 || jobs[0].path != path {
			t.Fatalf("single-file partitions = %#v, want one job index 0 path %q", jobs, path)
		}
	})

	t.Run("all_same_size", func(t *testing.T) {
		t.Parallel()
		repoRoot := t.TempDir()
		files := make([]string, 0, 12)
		for i := 0; i < 12; i++ {
			path := filepath.Join(repoRoot, "pkg", fmt.Sprintf("m_%02d.py", i))
			writeSizedFile(t, path, 500)
			files = append(files, path)
		}
		partitions := buildParseSubtreePartitions(repoRoot, files, 4)

		// Loss-free coverage.
		got := collectPartitionJobs(partitions)
		gotIndexes := make([]int, 0, len(got))
		for _, job := range got {
			gotIndexes = append(gotIndexes, job.index)
		}
		sort.Ints(gotIndexes)
		for i := range files {
			if gotIndexes[i] != i {
				t.Fatalf("all-same-size coverage index[%d] = %d, want %d", i, gotIndexes[i], i)
			}
		}

		// Equal sizes must balance evenly: no partition more than one file
		// heavier than another.
		minJobs, maxJobs := len(files), 0
		for _, partition := range partitions {
			if len(partition.jobs) < minJobs {
				minJobs = len(partition.jobs)
			}
			if len(partition.jobs) > maxJobs {
				maxJobs = len(partition.jobs)
			}
		}
		if maxJobs-minJobs > 1 {
			t.Fatalf("equal-size partitions unbalanced: min=%d max=%d", minJobs, maxJobs)
		}
	})
}
