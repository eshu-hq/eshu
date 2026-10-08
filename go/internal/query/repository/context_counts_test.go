// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestQueryRepositoryWorkloadCountUsesMaterializedGraphWithSummary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		graphCount int64
	}{
		{name: "retained identity without materialization", graphCount: 0},
		{name: "multiple materialized workloads", graphCount: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := graph.FakeRepoGraphReader{RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)") ||
					!strings.Contains(cypher, "RETURN count(DISTINCT w) AS count") || params["repo_id"] != "repo-1" {
					t.Fatalf("unexpected workload count query %q with params %#v", cypher, params)
				}
				return []map[string]any{{"count": tc.graphCount}}, nil
			}}
			readModelCounts := &repository.RepositoryReadModelCounts{Available: true}
			counts, err := queryRepositoryContextCounts(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil,
				&querycontract.RepositoryContentCoverage{Available: true}, readModelCounts)
			got := counts.workloadCount
			if err != nil || got != int(tc.graphCount) {
				t.Fatalf("workload count = %d, %v; want %d, nil", got, err, tc.graphCount)
			}
		})
	}
}

func TestQueryRepositoryWorkloadCountWithoutSummary(t *testing.T) {
	t.Parallel()
	reader := graph.FakeRepoGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "RETURN count(DISTINCT w) AS count") {
			t.Fatalf("unexpected workload count query %q", cypher)
		}
		return []map[string]any{{"count": int64(1)}}, nil
	}}
	got, err := queryRepositoryWorkloadCount(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil)
	if got != 1 || err != nil {
		t.Fatalf("workload count = %d, %v; want 1, nil", got, err)
	}
}

func TestQueryRepositoryWorkloadCountGraphErrorWithSummary(t *testing.T) {
	t.Parallel()
	reader := graph.FakeRepoGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		return nil, querycontract.ErrGraphReadDeadline
	}}
	counts, err := queryRepositoryContextCounts(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil,
		&querycontract.RepositoryContentCoverage{Available: true},
		&repository.RepositoryReadModelCounts{Available: true})
	got := counts.workloadCount
	if got != 0 || !errors.Is(err, querycontract.ErrGraphReadDeadline) {
		t.Fatalf("workload count = %d, %v; want 0, graph read deadline", got, err)
	}
}

// TestQueryRepositoryContextCountCallersAlwaysProjectACountAggregate guards
// queryRepositoryContextCount's unenforced contract (query-source-coverage.yaml
// disposition for this symbol): it takes a raw `cypher string` parameter and
// appends no LIMIT or shape check of its own -- the "single scalar count" bound
// this disposition asserts lives entirely in its 4 callers' literal Cypher, none
// of which has a digest of its own. A future 5th caller passing a
// non-aggregating Cypher would silently break the "count" row-key contract
// queryRepositoryContextCount relies on (`querycontract.IntVal(rows[0], "count")`) with no
// gate catching it. This test locks down the CURRENT 4 callers by asserting
// each one's actual Cypher text contains a `RETURN count(` aggregate
// projection.
func TestQueryRepositoryContextCountCallersAlwaysProjectACountAggregate(t *testing.T) {
	t.Parallel()

	assertCountAggregate := func(t *testing.T, run func(reader querycontract.GraphQuery) error) {
		t.Helper()
		var sawCypher string
		reader := graph.FakeRepoGraphReader{
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				sawCypher = cypher
				return []map[string]any{{"count": int64(1)}}, nil
			},
		}
		if err := run(reader); err != nil {
			t.Fatalf("caller error = %v, want nil", err)
		}
		if !strings.Contains(sawCypher, "RETURN count(") {
			t.Fatalf("cypher = %q, want it to contain a RETURN count( aggregate", sawCypher)
		}
	}

	t.Run("workload_count", func(t *testing.T) {
		t.Parallel()
		assertCountAggregate(t, func(reader querycontract.GraphQuery) error {
			_, err := queryRepositoryWorkloadCount(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil)
			return err
		})
	})
	t.Run("platform_count", func(t *testing.T) {
		t.Parallel()
		assertCountAggregate(t, func(reader querycontract.GraphQuery) error {
			_, err := queryRepositoryPlatformCount(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil)
			return err
		})
	})
	t.Run("dependency_count", func(t *testing.T) {
		t.Parallel()
		assertCountAggregate(t, func(reader querycontract.GraphQuery) error {
			_, err := queryRepositoryDependencyCount(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil)
			return err
		})
	})
	t.Run("file_count", func(t *testing.T) {
		t.Parallel()
		assertCountAggregate(t, func(reader querycontract.GraphQuery) error {
			_, err := queryRepositoryFileCount(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil)
			return err
		})
	})
}

// TestQueryRepositoryDependencyCountLaterPositionCallPropagatesGraphReadError
// covers the propagation-short-circuit gap this issue's review flagged: of
// queryRepositoryContextCounts's 4 sequential count calls
// (file/workload/platform/dependency, in that order), only the FIRST
// (file_count, via the sibling summary-counts sweep tests in
// graph_read_error_repository_context_aux_test.go) had a regression test
// proving a graph-read error aborts the whole aggregate. A short-circuit bug
// that only breaks propagation for a LATER call (for example an `if err !=
// nil` check accidentally dropped from the workload/platform/dependency
// branches specifically) would pass every existing test, since the first
// call's own error never reaches those branches. This test lets file_count
// and workload_count succeed and fails platform_count, proving the error from
// a later-position call still aborts the aggregate and propagates unchanged.
func TestQueryRepositoryDependencyCountLaterPositionCallPropagatesGraphReadError(t *testing.T) {
	t.Parallel()

	reader := graph.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "REPO_CONTAINS]->(f:File)") && strings.Contains(cypher, "count(DISTINCT f)"):
				return []map[string]any{{"count": int64(3)}}, nil
			case strings.Contains(cypher, "DEFINES]->(w:Workload)") && strings.Contains(cypher, "count(DISTINCT w)"):
				return []map[string]any{{"count": int64(2)}}, nil
			case strings.Contains(cypher, "RUNS_ON]->(p:Platform)"):
				return nil, querycontract.ErrGraphReadDeadline
			default:
				t.Fatalf("unexpected cypher reached after platform_count should have aborted: %q", cypher)
				return nil, nil
			}
		},
	}

	_, err := queryRepositoryContextCounts(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil, nil)
	if err == nil {
		t.Fatal("queryRepositoryContextCounts() error = nil, want querycontract.ErrGraphReadDeadline propagated from platform_count")
	}
	if !strings.Contains(err.Error(), querycontract.ErrGraphReadDeadline.Error()) {
		t.Fatalf("queryRepositoryContextCounts() error = %v, want it to wrap querycontract.ErrGraphReadDeadline", err)
	}
}
