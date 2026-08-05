// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

// TestQueryRepositoryContextCountCallersAlwaysProjectACountAggregate guards
// queryRepositoryContextCount's unenforced contract (query-source-coverage.yaml
// disposition for this symbol): it takes a raw `cypher string` parameter and
// appends no LIMIT or shape check of its own -- the "single scalar count" bound
// this disposition asserts lives entirely in its 4 callers' literal Cypher, none
// of which has a digest of its own. A future 5th caller passing a
// non-aggregating Cypher would silently break the "count" row-key contract
// queryRepositoryContextCount relies on (`IntVal(rows[0], "count")`) with no
// gate catching it. This test locks down the CURRENT 4 callers by asserting
// each one's actual Cypher text contains a `RETURN count(` aggregate
// projection.
func TestQueryRepositoryContextCountCallersAlwaysProjectACountAggregate(t *testing.T) {
	t.Parallel()

	assertCountAggregate := func(t *testing.T, run func(reader GraphQuery) error) {
		t.Helper()
		var sawCypher string
		reader := fakeRepoGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
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
		assertCountAggregate(t, func(reader GraphQuery) error {
			_, err := queryRepositoryWorkloadCount(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil)
			return err
		})
	})
	t.Run("platform_count", func(t *testing.T) {
		t.Parallel()
		assertCountAggregate(t, func(reader GraphQuery) error {
			_, err := queryRepositoryPlatformCount(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil)
			return err
		})
	})
	t.Run("dependency_count", func(t *testing.T) {
		t.Parallel()
		assertCountAggregate(t, func(reader GraphQuery) error {
			_, err := queryRepositoryDependencyCount(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil)
			return err
		})
	})
	t.Run("file_count", func(t *testing.T) {
		t.Parallel()
		assertCountAggregate(t, func(reader GraphQuery) error {
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

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "REPO_CONTAINS]->(f:File)") && strings.Contains(cypher, "count(DISTINCT f)"):
				return []map[string]any{{"count": int64(3)}}, nil
			case strings.Contains(cypher, "DEFINES]->(w:Workload)") && strings.Contains(cypher, "count(DISTINCT w)"):
				return []map[string]any{{"count": int64(2)}}, nil
			case strings.Contains(cypher, "RUNS_ON]->(p:Platform)"):
				return nil, ErrGraphReadDeadline
			default:
				t.Fatalf("unexpected cypher reached after platform_count should have aborted: %q", cypher)
				return nil, nil
			}
		},
	}

	_, err := queryRepositoryContextCounts(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil, nil)
	if err == nil {
		t.Fatal("queryRepositoryContextCounts() error = nil, want ErrGraphReadDeadline propagated from platform_count")
	}
	if !strings.Contains(err.Error(), ErrGraphReadDeadline.Error()) {
		t.Fatalf("queryRepositoryContextCounts() error = %v, want it to wrap ErrGraphReadDeadline", err)
	}
}
