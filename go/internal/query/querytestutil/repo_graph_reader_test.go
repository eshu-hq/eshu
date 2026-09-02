// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestFakeRepoGraphReaderRunMatchesLongestFragment pins the dispatch rule a
// test relies on when it registers both a general and a more specific
// fragment: the longer, more specific one wins rather than whichever the map
// iterates first.
func TestFakeRepoGraphReaderRunMatchesLongestFragment(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeRepoGraphReader{
		RunByMatch: map[string][]map[string]any{
			"MATCH (r:Repository)":               {{"which": "general"}},
			"MATCH (r:Repository)-[:DEPLOYS_TO]": {{"which": "specific"}},
		},
	}

	got, err := reader.Run(context.Background(), "MATCH (r:Repository)-[:DEPLOYS_TO]->(p)", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(got) != 1 || got[0]["which"] != "specific" {
		t.Fatalf("Run() = %v, want the longer-fragment rows", got)
	}
}

// TestFakeRepoGraphReaderRunSingleSingleEntryFallback pins the fallback that
// makes FakeRepoGraphReader different from FakeWorkloadGraphReader: when no
// fragment matches, the query is the narrow single-repository lookup, and
// exactly one row is registered, RunSingle returns that sole row rather than
// nil. Losing this silently breaks every repository test that registers its
// row without keying it to the exact lookup fragment.
func TestFakeRepoGraphReaderRunSingleSingleEntryFallback(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeRepoGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"some fragment the query text does not contain": {"id": "repo-1"},
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (r:Repository {id: $repo_id}) RETURN r", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row["id"] != "repo-1" {
		t.Fatalf("RunSingle() = %v, want the sole registered row via the single-entry fallback", row)
	}
}

// TestFakeRepoGraphReaderRunSingleFallbackNeedsExactlyOneEntry guards the
// fallback from over-firing: with two or more rows registered, an unmatched
// single-repository lookup must not guess which one to return.
func TestFakeRepoGraphReaderRunSingleFallbackNeedsExactlyOneEntry(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeRepoGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"fragment-a": {"id": "repo-a"},
			"fragment-b": {"id": "repo-b"},
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (r:Repository {id: $repo_id}) RETURN r", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row != nil {
		t.Fatalf("RunSingle() = %v, want nil when more than one row is registered", row)
	}
}

// TestFakeRepoGraphReaderRunSingleFallbackNeedsTheRepositoryLookup guards the
// fallback from firing on an unrelated unmatched query: it must only stand in
// for the narrow single-repository lookup, not any query that fails to match.
func TestFakeRepoGraphReaderRunSingleFallbackNeedsTheRepositoryLookup(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeRepoGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"fragment-a": {"id": "repo-a"},
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (w:Workload {id: $workload_id}) RETURN w", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row != nil {
		t.Fatalf("RunSingle() = %v, want nil for a query the fallback does not cover", row)
	}
}

// TestFakeRepoGraphReaderRunFnOverridesRunByMatch confirms RunFn, when set,
// answers directly rather than consulting RunByMatch.
func TestFakeRepoGraphReaderRunFnOverridesRunByMatch(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeRepoGraphReader{
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

// TestFakeRepoGraphReaderRunSingleFnOverridesFallback confirms RunSingleFn,
// when set, answers directly and bypasses the single-entry fallback entirely.
func TestFakeRepoGraphReaderRunSingleFnOverridesFallback(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeRepoGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"unrelated": {"id": "should-not-surface"},
		},
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"id": "run-single-fn"}, nil
		},
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (r:Repository {id: $repo_id}) RETURN r", nil)
	if err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}
	if row["id"] != "run-single-fn" {
		t.Fatalf("RunSingle() = %v, want the RunSingleFn answer", row)
	}
}

// TestFakeRepoGraphReaderZeroValueIsUsable matters because a handler test can
// construct the fake with no fields at all, purely to satisfy a port.
func TestFakeRepoGraphReaderZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var reader querytestutil.FakeRepoGraphReader

	rows, err := reader.Run(context.Background(), "MATCH (n) RETURN n", nil)
	if err != nil || rows != nil {
		t.Fatalf("Run() = (%v, %v), want (nil, nil)", rows, err)
	}

	row, err := reader.RunSingle(context.Background(), "MATCH (r:Repository {id: $repo_id})", nil)
	if err != nil || row != nil {
		t.Fatalf("RunSingle() = (%v, %v), want (nil, nil)", row, err)
	}
}
