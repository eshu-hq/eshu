// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestFakeWorkloadGraphReaderRunMatchesLongestFragment pins the same
// longest-fragment dispatch rule FakeRepoGraphReader uses, so a workload test
// can register a general and a more specific fragment without the general one
// shadowing the specific one.
func TestFakeWorkloadGraphReaderRunMatchesLongestFragment(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeWorkloadGraphReader{
		RunByMatch: map[string][]map[string]any{
			"MATCH (w:Workload)":            {{"which": "general"}},
			"MATCH (w:Workload)-[:RUNS_ON]": {{"which": "specific"}},
		},
	}

	got, err := reader.Run(context.Background(), "MATCH (w:Workload)-[:RUNS_ON]->(p)", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(got) != 1 || got[0]["which"] != "specific" {
		t.Fatalf("Run() = %v, want the longer-fragment rows", got)
	}
}

// TestFakeWorkloadGraphReaderRunSingleHasNoSingleEntryFallback pins the
// asymmetry against FakeRepoGraphReader: a single registered row must NOT
// surface for an unmatched query, even one shaped like a narrow single-entity
// lookup. FakeWorkloadGraphReader has no such fallback, and this test fails if
// one is ever copied in from FakeRepoGraphReader.
func TestFakeWorkloadGraphReaderRunSingleHasNoSingleEntryFallback(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeWorkloadGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"some fragment the query text does not contain": {"id": "workload-1"},
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (w:Workload {id: $workload_id}) RETURN w", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row != nil {
		t.Fatalf("RunSingle() = %v, want nil: FakeWorkloadGraphReader has no single-entry fallback", row)
	}
}

// TestFakeWorkloadGraphReaderRunSingleFallbackAbsentEvenForRepositoryLookup
// covers the exact query text FakeRepoGraphReader's fallback keys on. If that
// fallback is ever merged into a shared type, this is the case most likely to
// keep compiling and start passing for the wrong reason.
func TestFakeWorkloadGraphReaderRunSingleFallbackAbsentEvenForRepositoryLookup(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeWorkloadGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"unrelated fragment": {"id": "workload-1"},
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (r:Repository {id: $repo_id}) RETURN r", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row != nil {
		t.Fatalf("RunSingle() = %v, want nil even for the repository-lookup shape", row)
	}
}

// TestFakeWorkloadGraphReaderRunFnOverridesRunByMatch confirms RunFn, when
// set, answers directly rather than consulting RunByMatch.
func TestFakeWorkloadGraphReaderRunFnOverridesRunByMatch(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeWorkloadGraphReader{
		RunByMatch: map[string][]map[string]any{
			"MATCH": {{"which": "by-match"}},
		},
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"which": "run-fn"}}, nil
		},
	}

	got, err := reader.Run(context.Background(), "MATCH (n) RETURN n", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(got) != 1 || got[0]["which"] != "run-fn" {
		t.Fatalf("Run() = %v, want the RunFn answer", got)
	}
}

// TestFakeWorkloadGraphReaderRunSingleFnOverridesByMatch confirms
// RunSingleFn, when set, answers directly rather than consulting
// RunSingleByMatch.
func TestFakeWorkloadGraphReaderRunSingleFnOverridesByMatch(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeWorkloadGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"unrelated": {"id": "should-not-surface"},
		},
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"id": "run-single-fn"}, nil
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (w:Workload {id: $workload_id}) RETURN w", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row["id"] != "run-single-fn" {
		t.Fatalf("RunSingle() = %v, want the RunSingleFn answer", row)
	}
}

// TestFakeWorkloadGraphReaderZeroValueIsUsable matters because a handler test
// can construct the fake with no fields at all, purely to satisfy a port.
func TestFakeWorkloadGraphReaderZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var reader querytestutil.FakeWorkloadGraphReader

	rows, err := reader.Run(context.Background(), "MATCH (n) RETURN n", nil)
	if err != nil || rows != nil {
		t.Fatalf("Run() = (%v, %v), want (nil, nil)", rows, err)
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (w:Workload {id: $workload_id})", nil)
	if err != nil || row != nil {
		t.Fatalf("RunSingle() = (%v, %v), want (nil, nil)", row, err)
	}
}
