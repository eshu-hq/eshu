// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"hash/fnv"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestConfigWithDefaults(t *testing.T) {
	t.Parallel()

	got := Config{}.WithDefaults()
	want := Config{
		Workers:           DefaultWorkers,
		PageBudget:        DefaultPageBudget,
		SkipAfterFailures: DefaultSkipAfterFailures,
		PollInterval:      DefaultPollInterval,
	}
	if got != want {
		t.Fatalf("Config{}.WithDefaults() = %+v, want %+v", got, want)
	}
	if DefaultWorkers != 4 || DefaultSkipAfterFailures != 5 {
		t.Fatalf("defaults = %d workers, skip after %d; the record names 4 and 5", DefaultWorkers, DefaultSkipAfterFailures)
	}
	set := Config{Workers: 2, PageBudget: 10, SkipAfterFailures: 3, PollInterval: time.Second}
	if got := set.WithDefaults(); got != set {
		t.Fatalf("WithDefaults() changed set values: %+v, want %+v", got, set)
	}
	negative := Config{Workers: -1, PageBudget: -1, SkipAfterFailures: -1, PollInterval: -time.Second}
	if got := negative.WithDefaults(); got != want {
		t.Fatalf("WithDefaults() kept non-positive values: %+v, want %+v", got, want)
	}
}

// TestAssignPartitionsByRepository pins the walker's work split: every
// repository lands on exactly one worker, a repository always lands on the
// same worker whatever else is in the page, a repeated id is assigned once,
// and each worker keeps input order.
func TestAssignPartitionsByRepository(t *testing.T) {
	t.Parallel()

	repos := make([]string, 0, 200)
	for i := range 200 {
		repos = append(repos, fmt.Sprintf("repository:r_%03d", i))
	}
	input := append(append([]string{}, repos...), repos[3], repos[150])
	parts := Assign(input, 4)
	if len(parts) != 4 {
		t.Fatalf("Assign() returned %d workers, want 4", len(parts))
	}

	owner := map[string]int{}
	var all []string
	for worker, part := range parts {
		if len(part) == 0 {
			t.Errorf("worker %d got no work over 200 repositories", worker)
		}
		lastIndex := -1
		for _, repo := range part {
			if prev, ok := owner[repo]; ok {
				t.Fatalf("repository %s assigned to workers %d and %d", repo, prev, worker)
			}
			owner[repo] = worker
			all = append(all, repo)
			index := sort.SearchStrings(repos, repo)
			if index <= lastIndex {
				t.Fatalf("worker %d lost input order at %s", worker, repo)
			}
			lastIndex = index
		}
	}
	sort.Strings(all)
	if !reflect.DeepEqual(all, repos) {
		t.Fatalf("Assign() covered %d repositories, want each of %d exactly once", len(all), len(repos))
	}

	// The same repository goes to the same worker in any other page.
	for _, repo := range []string{repos[0], repos[77], repos[199]} {
		single := Assign([]string{repo, "repository:other"}, 4)
		for worker, part := range single {
			for _, got := range part {
				if got == repo && worker != owner[repo] {
					t.Fatalf("repository %s moved from worker %d to %d", repo, owner[repo], worker)
				}
			}
		}
	}
}

func TestAssignEdgeCases(t *testing.T) {
	t.Parallel()

	if got := Assign(nil, 3); len(got) != 3 || len(got[0])+len(got[1])+len(got[2]) != 0 {
		t.Fatalf("Assign(nil, 3) = %v, want three empty workers", got)
	}
	if got := Assign([]string{"a", "b"}, 0); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"a", "b"}) {
		t.Fatalf("Assign(_, 0) = %v, want one worker with every repository", got)
	}
}

// frozenWorkerFor is a frozen copy of the mapping as it shipped in PR3b-1a:
// FNV-1a over repo_id, modulo the worker count in uint32. Two processes of
// different releases must agree on it, so workerFor must equal it for every
// worker count that fits in uint32.
func frozenWorkerFor(repoID string, workers int) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(repoID))
	return int(hash.Sum32() % uint32(workers)) // #nosec G115 -- test-only frozen copy of the shipped formula; workers is 1..MaxInt32 here
}

// TestWorkerForMappingIsFrozen pins the repo_id-to-worker mapping with literal
// values computed from the shipped formula, and checks workerFor against the
// frozen copy over many ids for every worker count from 1 to 64 and for
// math.MaxInt32.
func TestWorkerForMappingIsFrozen(t *testing.T) {
	t.Parallel()

	counts := []int{1, 2, 3, 7, 16, 64, math.MaxInt32}
	pinned := []struct {
		repoID string
		want   []int
	}{
		{"", []int{0, 1, 1, 2, 5, 5, 18652614}},
		{"a", []int{0, 0, 1, 5, 12, 44, 1678518573}},
		{"repository:r_000", []int{0, 0, 2, 2, 2, 18, 1681701842}},
		{"repository:r_077", []int{0, 0, 1, 2, 8, 40, 1866748521}},
		{"repository:r_199", []int{0, 1, 2, 6, 7, 39, 1441093288}},
		{"repo-github.com/eshu-hq/eshu", []int{0, 0, 1, 5, 0, 32, 2019602464}},
		{"日本/リポ", []int{0, 1, 1, 2, 11, 43, 219778603}},
	}
	for _, p := range pinned {
		for i, workers := range counts {
			if got := workerFor(p.repoID, workers); got != p.want[i] {
				t.Errorf("workerFor(%q, %d) = %d, want %d", p.repoID, workers, got, p.want[i])
			}
		}
	}
	for i := range 2000 {
		repoID := fmt.Sprintf("repository:r_%04d", i)
		for _, workers := range append([]int{math.MaxInt32}, rangeInts(1, 64)...) {
			if got, want := workerFor(repoID, workers), frozenWorkerFor(repoID, workers); got != want {
				t.Fatalf("workerFor(%q, %d) = %d, frozen formula = %d", repoID, workers, got, want)
			}
		}
	}
}

// TestWorkerForStaysInRangeAboveUint32 covers worker counts the uint32 form
// could not hold (it truncated them, and 1<<32 divided by zero). The result
// must still be a valid worker index.
func TestWorkerForStaysInRangeAboveUint32(t *testing.T) {
	t.Parallel()

	if math.MaxInt <= math.MaxUint32 {
		t.Skip("int is 32 bits on this platform")
	}
	for _, workers := range []int{math.MaxUint32, math.MaxUint32 + 1, math.MaxUint32 + 2, math.MaxInt} {
		got := workerFor("repository:r_000", workers)
		if got < 0 || got >= workers {
			t.Fatalf("workerFor(_, %d) = %d, want 0 <= index < workers", workers, got)
		}
	}
}

func rangeInts(lo, hi int) []int {
	out := make([]int, 0, hi-lo+1)
	for v := lo; v <= hi; v++ {
		out = append(out, v)
	}
	return out
}
