// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// exactCountConsumerSearchContentStore answers the exact-case service-name
// consumer search with exactly N rows no matter what limit the caller passes,
// modeling a backend that holds exactly N matching file rows. The production
// +1 probe must report truncated only when more rows exist than the caller
// asked for, not when the backend returned exactly the asked number.
type exactCountConsumerSearchContentStore struct {
	content.FakePortContentStore
	pattern string
	count   int
}

func (s exactCountConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(
	_ context.Context,
	pattern string,
	limit int,
) ([]querycontract.FileContent, error) {
	if pattern != s.pattern || limit <= 0 {
		return nil, nil
	}
	rows := make([]querycontract.FileContent, 0, s.count)
	for index := range s.count {
		rows = append(rows, querycontract.FileContent{
			RepoID:       fmt.Sprintf("repository:exact-%02d", index),
			RelativePath: fmt.Sprintf("deploy/values-%02d.yaml", index),
		})
	}
	return rows, nil
}

func emptyGraphReader() graph.FakeGraphReader {
	return graph.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, nil
		},
	}
}

// TestPerSearchExactFullPageIsNotTruncated is the #10535 follow-up to #10477:
// searchConsumerEvidenceAnyRepo reports truncated when a per-search content
// read returns exactly the caller's limit, even though nothing was dropped.
// Only a read that returns MORE than the limit proves truncation. The +1
// probe (fetch limit+1, truncate on len > limit) fixes the false positive;
// this test reds on the len >= limit comparison.
func TestPerSearchExactFullPageIsNotTruncated(t *testing.T) {
	t.Parallel()

	const limit = 5

	t.Run("exactly limit rows is complete", func(t *testing.T) {
		t.Parallel()

		consumers, truncated, err := deployment.LoadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(),
			emptyGraphReader(),
			exactCountConsumerSearchContentStore{pattern: "exact-svc", count: limit},
			"repository:exact-svc",
			"exact-svc",
			nil,
			limit,
			nil,
			false,
			false,
		)
		if err != nil {
			t.Fatalf("LoadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if truncated {
			t.Fatalf("truncated = true, want false (backend returned exactly the %d asked rows; the +1 probe must prove more exist)", limit)
		}
		if got, want := len(consumers), limit; got != want {
			t.Fatalf("len(consumers) = %d, want %d (no row may be dropped by the probe)", got, want)
		}
	})

	t.Run("limit plus one rows is truncated", func(t *testing.T) {
		t.Parallel()

		consumers, truncated, err := deployment.LoadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(),
			emptyGraphReader(),
			exactCountConsumerSearchContentStore{pattern: "exact-svc", count: limit + 1},
			"repository:exact-svc",
			"exact-svc",
			nil,
			limit,
			nil,
			false,
			false,
		)
		if err != nil {
			t.Fatalf("LoadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true (%d rows exceed the %d-row bound)", limit+1, limit)
		}
		if got, want := len(consumers), limit; got != want {
			t.Fatalf("len(consumers) = %d, want %d (trimmed to the bound)", got, want)
		}
	})
}
