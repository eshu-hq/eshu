// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
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
