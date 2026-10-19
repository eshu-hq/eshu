// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
)

// wideTFFiles builds n Terraform source files whose names sort in creation
// order for any n below 100,000, so file n-1 is the last in list order. Each
// yields one module row.
func wideTFFiles(n int) []File {
	files := make([]File, 0, n)
	for i := 0; i < n; i++ {
		files = append(files, fileRow(
			fmt.Sprintf("infra/m%05d/main.tf", i),
			fmt.Sprintf("module \"m%d\" {\n  source = \"./mod%d\"\n}\n", i, i),
		))
	}
	return files
}

// yamlFiles returns n plain YAML candidates that no extractor reads, so the
// load blank-checks them instead of reading their content.
func yamlFiles(n int) []File {
	files := make([]File, 0, n)
	for i := 0; i < n; i++ {
		files = append(files, fileRow(fmt.Sprintf("cfg/f%05d.yaml", i), "k: v\n"))
	}
	return files
}

func TestSelectHydrationIndexesHonorsTheAllowance(t *testing.T) {
	t.Parallel()

	files := make([]File, 0, HydrationFloor+10)
	for i := 0; i < HydrationFloor+10; i++ {
		files = append(files, fileRow(fmt.Sprintf("a-%03d.yaml", i), ""))
	}
	// A candidate that already carries content is not part of the pool.
	files[3].Content = "k: v\n"
	// A file that is no config-artifact candidate is not part of the pool.
	files[7].RelativePath = "README.md"
	pool := HydrationFloor + 10 - 2

	for _, tc := range []struct {
		name      string
		allowance int
		want      int
	}{
		{"floor", HydrationFloor, HydrationFloor},
		{"zero", 0, 0},
		{"one past the pool", pool + 1, pool},
		{"whole window", AllowanceWholeWindow, pool},
	} {
		indexes, candidates := SelectHydrationIndexes(files, isConfigArtifactCandidate, fileContent, tc.allowance)
		if candidates != pool {
			t.Fatalf("%s: candidates = %d, want the pool %d", tc.name, candidates, pool)
		}
		if len(indexes) != tc.want {
			t.Fatalf("%s: selected %d, want %d", tc.name, len(indexes), tc.want)
		}
		if !sort.IntsAreSorted(indexes) {
			t.Fatalf("%s: selection %v is not in list order", tc.name, indexes)
		}
		for _, index := range indexes {
			if index == 3 || index == 7 {
				t.Fatalf("%s: selected position %d, which is not in the pool", tc.name, index)
			}
		}
	}
}

// TestLoadConfigPathsReadsTheListWithASentinel is the L1 contract: a
// repository list is read with ListLimit+1 rows; a sentinel row clips the
// list to ListLimit and sets ListTruncated, and the clipped file is never
// read. Exactly ListLimit files is a complete list.
func TestLoadConfigPathsReadsTheListWithASentinel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		files         int
		wantTruncated bool
	}{
		{ListLimit - 1, false}, {ListLimit, false}, {ListLimit + 1, true}, {ListLimit + 7, true},
	} {
		tc := tc
		t.Run(fmt.Sprintf("%d files", tc.files), func(t *testing.T) {
			t.Parallel()
			files := wideTFFiles(tc.files)
			store := newHydrationStore(files...)
			_, stats, err := LoadConfigPaths(context.Background(), store,
				Repository{ID: hydrationTestRepoID, Name: "svc"}, nil, false, AllowanceWholeWindow)
			if err != nil {
				t.Fatalf("load error = %v", err)
			}
			if stats.ListTruncated != tc.wantTruncated {
				t.Fatalf("ListTruncated = %v, want %v", stats.ListTruncated, tc.wantTruncated)
			}
			wantListed := min(tc.files, ListLimit)
			if stats.FilesListed != wantListed {
				t.Fatalf("FilesListed = %d, want %d", stats.FilesListed, wantListed)
			}
			if stats.CandidateCount != wantListed || stats.CandidatesHydrated != wantListed {
				t.Fatalf("candidates = %d hydrated = %d, want %d of the window", stats.CandidateCount, stats.CandidatesHydrated, wantListed)
			}
			if tc.files > ListLimit {
				beyond := files[ListLimit].RelativePath
				for _, call := range store.getCalls {
					if call == beyond {
						t.Fatalf("read %q, which sits past the %d-file window", beyond, ListLimit)
					}
				}
			}
		})
	}
}

// TestLoadConfigPathsAllowanceBoundsTheSelection proves the allowance is the
// only per-repository bound: a positive allowance selects that many
// candidates, AllowanceWholeWindow selects every candidate of the window, and
// the counts say how many were left.
func TestLoadConfigPathsAllowanceBoundsTheSelection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		candidates int
		allowance  int
		want       int
	}{
		{"51 under the whole window", 51, AllowanceWholeWindow, 51},
		{"500 under the whole window", 500, AllowanceWholeWindow, 500},
		{"500 at the floor", 500, HydrationFloor, HydrationFloor},
		{"500 at 120", 500, 120, 120},
		{"30 at the floor", 30, HydrationFloor, 30},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newHydrationStore(wideTFFiles(tc.candidates)...)
			rows, stats, err := LoadConfigPaths(context.Background(), store,
				Repository{ID: hydrationTestRepoID, Name: "svc"}, nil, false, tc.allowance)
			if err != nil {
				t.Fatalf("load error = %v", err)
			}
			if stats.CandidateCount != tc.candidates {
				t.Fatalf("CandidateCount = %d, want %d", stats.CandidateCount, tc.candidates)
			}
			if stats.CandidatesHydrated != tc.want {
				t.Fatalf("CandidatesHydrated = %d, want %d", stats.CandidatesHydrated, tc.want)
			}
			// Each Terraform file yields one module row, so rows track the
			// selection exactly.
			if len(rows) != tc.want {
				t.Fatalf("rows = %d, want one per selected Terraform file (%d)", len(rows), tc.want)
			}
			if wantChunks := (tc.want + ReadChunkSize - 1) / ReadChunkSize; stats.BatchStatements != wantChunks {
				t.Fatalf("BatchStatements = %d, want ceil(%d/%d) = %d", stats.BatchStatements, tc.want, ReadChunkSize, wantChunks)
			}
		})
	}
}

// TestLoadConfigPathsRejectsAnAllowanceBelowTheWholeWindow keeps a bad budget
// from reading as an unbounded one: only AllowanceWholeWindow means "every
// candidate".
func TestLoadConfigPathsRejectsAnAllowanceBelowTheWholeWindow(t *testing.T) {
	t.Parallel()

	store := newHydrationStore(wideTFFiles(3)...)
	_, _, err := LoadConfigPaths(context.Background(), store,
		Repository{ID: hydrationTestRepoID, Name: "svc"}, nil, false, AllowanceWholeWindow-1)
	if err == nil {
		t.Fatal("allowance -2 was accepted; it must fail instead of reading the whole window")
	}
	if store.listCalls != 0 {
		t.Fatalf("list statements = %d, want none before the allowance is checked", store.listCalls)
	}
}

// TestBlankCheckIsChunkedAtFiveHundredPaths pins the blank-check statement
// shape: at most BlankCheckChunkSize paths per NonBlankPaths statement, in
// sorted path order, every deferred path checked exactly once, and one
// BlankCheckStatements count per statement.
func TestBlankCheckIsChunkedAtFiveHundredPaths(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		files, wantStatements int
	}{
		{1, 1}, {499, 1}, {500, 1}, {501, 2}, {1171, 3}, {4950, 10}, {5000, 10},
	} {
		tc := tc
		t.Run(fmt.Sprintf("%d deferred paths", tc.files), func(t *testing.T) {
			t.Parallel()
			files := yamlFiles(tc.files)
			files[0].Content = " \n"
			store := newHydrationStore(files...)
			rows, stats, err := LoadConfigPaths(context.Background(), store,
				Repository{ID: hydrationTestRepoID, Name: "svc"}, nil, false, AllowanceWholeWindow)
			if err != nil {
				t.Fatalf("load error = %v", err)
			}
			_ = rows
			if len(store.blankCalls) != tc.wantStatements || stats.BlankCheckStatements != tc.wantStatements {
				t.Fatalf("blank-check statements = %d (stats %d), want %d", len(store.blankCalls), stats.BlankCheckStatements, tc.wantStatements)
			}
			var all []string
			for _, call := range store.blankCalls {
				if len(call) > BlankCheckChunkSize {
					t.Fatalf("blank check named %d paths, want at most %d", len(call), BlankCheckChunkSize)
				}
				all = append(all, call...)
			}
			if len(all) != tc.files {
				t.Fatalf("blank check named %d paths in total, want each of %d once", len(all), tc.files)
			}
			if !sort.StringsAreSorted(all) {
				t.Fatal("blank-check paths are not in sorted order across chunks")
			}
		})
	}
}

// TestBlankCheckChunkFailureFailsTheRepository keeps the failure rule of the
// single statement: any failed chunk fails the repository's load, and the
// statements issued up to it are counted.
func TestBlankCheckChunkFailureFailsTheRepository(t *testing.T) {
	t.Parallel()

	store := newHydrationStore(yamlFiles(1200)...)
	store.blankErrAfter = 1
	store.blankErr = errInjectedRead
	_, stats, err := LoadConfigPaths(context.Background(), store,
		Repository{ID: hydrationTestRepoID, Name: "svc"}, nil, false, AllowanceWholeWindow)
	if err == nil {
		t.Fatal("a failed second blank-check chunk did not fail the load")
	}
	if !errors.Is(err, errInjectedRead) {
		t.Fatalf("err = %v, want it to wrap the injected read failure", err)
	}
	if stats.BlankCheckStatements != 2 {
		t.Fatalf("BlankCheckStatements = %d, want the 2 statements issued", stats.BlankCheckStatements)
	}
}
