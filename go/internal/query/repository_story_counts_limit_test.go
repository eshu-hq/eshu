// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestQueryRepositoryStoryStringRowsBoundsRowsWithNamedLimit is the LIMIT
// contract guard for #5764's new bound on this shared helper (backing
// queryRepositoryStoryWorkloadNames, queryRepositoryStoryPlatformTypes, and
// queryRepositoryStoryLanguages): the Cypher must carry a literal
// "LIMIT $limit" suffix and params["limit"] must equal
// repositoryStoryStringRowLimit+1 (P1 review follow-up to #5764: requesting
// one extra row over the disclosed limit is what lets the caller detect
// truncation exactly, via len(rows) > repositoryStoryStringRowLimit, instead
// of the ambiguous len(rows) == limit check). Deleting either the
// "\n\t\t\tLIMIT $limit" append or the queryParams["limit"] assignment in
// production must fail this test -- the fakes used by the other tests on
// this route ignore params["limit"] entirely and would pass either way.
func TestQueryRepositoryStoryStringRowsBoundsRowsWithNamedLimit(t *testing.T) {
	t.Parallel()

	var sawLimitClause bool
	var sawLimitParam any
	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			sawLimitClause = strings.Contains(cypher, "LIMIT $limit")
			sawLimitParam = params["limit"]
			return nil, nil
		},
	}

	_, _, err := queryRepositoryStoryStringRows(
		t.Context(),
		reader,
		map[string]any{"repo_id": "repo-1"},
		"workload_names",
		"workload_name",
		`MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload) RETURN w.name AS workload_name`,
		nil,
	)
	if err != nil {
		t.Fatalf("queryRepositoryStoryStringRows() error = %v, want nil", err)
	}
	if !sawLimitClause {
		t.Fatal("cypher missing \"LIMIT $limit\" clause")
	}
	if sawLimitParam != repositoryStoryStringRowLimit+1 {
		t.Fatalf("params[limit] = %#v, want %d (repositoryStoryStringRowLimit+1)", sawLimitParam, repositoryStoryStringRowLimit+1)
	}
}

// TestQueryRepositoryStoryStringRowsDetectsExactTruncation is the semantic
// guard for the P1 review follow-up to #5764: queryRepositoryStoryStringRows
// must report truncated=true, and cap the returned values to
// repositoryStoryStringRowLimit, exactly when the read returns MORE than the
// limit -- not when it returns exactly the limit. This is the exact-detection
// property the limit+1 request buys; a mutant reverting to the ambiguous
// len(rows) == limit check flips both boundary cases below and must fail this
// test.
func TestQueryRepositoryStoryStringRowsDetectsExactTruncation(t *testing.T) {
	t.Parallel()

	newReaderWithRowCount := func(rowCount int) fakeRepoGraphReader {
		return fakeRepoGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				limit := IntVal(params, "limit")
				rows := make([]map[string]any, 0, rowCount)
				for i := 0; i < rowCount && i < limit; i++ {
					rows = append(rows, map[string]any{"workload_name": fmt.Sprintf("workload-%03d", i)})
				}
				return rows, nil
			},
		}
	}

	t.Run("exactly at the limit is not truncated", func(t *testing.T) {
		t.Parallel()
		values, truncated, err := queryRepositoryStoryStringRows(
			t.Context(),
			newReaderWithRowCount(repositoryStoryStringRowLimit),
			map[string]any{"repo_id": "repo-1"},
			"workload_names",
			"workload_name",
			`MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload) RETURN DISTINCT w.name AS workload_name`,
			nil,
		)
		if err != nil {
			t.Fatalf("queryRepositoryStoryStringRows() error = %v, want nil", err)
		}
		if truncated {
			t.Fatalf("truncated = true, want false when exactly %d rows exist", repositoryStoryStringRowLimit)
		}
		if len(values) != repositoryStoryStringRowLimit {
			t.Fatalf("len(values) = %d, want %d", len(values), repositoryStoryStringRowLimit)
		}
	})

	t.Run("one past the limit is truncated and capped", func(t *testing.T) {
		t.Parallel()
		values, truncated, err := queryRepositoryStoryStringRows(
			t.Context(),
			newReaderWithRowCount(repositoryStoryStringRowLimit+1),
			map[string]any{"repo_id": "repo-1"},
			"workload_names",
			"workload_name",
			`MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload) RETURN DISTINCT w.name AS workload_name`,
			nil,
		)
		if err != nil {
			t.Fatalf("queryRepositoryStoryStringRows() error = %v, want nil", err)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true when %d rows exist", repositoryStoryStringRowLimit+1)
		}
		if len(values) != repositoryStoryStringRowLimit {
			t.Fatalf("len(values) = %d, want %d (capped)", len(values), repositoryStoryStringRowLimit)
		}
	})
}
