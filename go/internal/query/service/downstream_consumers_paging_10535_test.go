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

// TestListServiceDownstreamConsumersSearchedHostnamesNeverNull pins the
// OpenAPI contract: searched_hostnames is a non-nullable array, so a service
// with no hostnames must disclose an empty list, not null.
func TestListServiceDownstreamConsumersSearchedHostnamesNeverNull(t *testing.T) {
	t.Parallel()

	page, err := ListServiceDownstreamConsumers(
		context.Background(), emptyGraphReader(), listTestContentStore{total: 0},
		"repository:svc", "svc",
		listTestAccess(), nil, 50,
	)
	if err != nil {
		t.Fatalf("ListServiceDownstreamConsumers() error = %v, want nil", err)
	}
	if page.SearchedHostnames == nil {
		t.Fatal("SearchedHostnames = nil, want empty non-nil slice per the OpenAPI array contract")
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

	t.Run("unindexed fallback keeps the legacy cap under a widened read", func(t *testing.T) {
		t.Parallel()

		// The substring fallback has no index to seek with: at a widened
		// internal bound it would scan and sort hundreds of rows, measured
		// in seconds on QA-scale content, so it keeps the legacy 100-row
		// cap while the +1 probe still reports honestly.
		const widened = 500
		consumers, truncated, err := deployment.LoadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(),
			emptyGraphReader(),
			exactCountConsumerSearchContentStore{pattern: "exact-svc", count: 150},
			"repository:exact-svc",
			"exact-svc",
			nil,
			widened,
			nil,
			false,
			false,
		)
		if err != nil {
			t.Fatalf("LoadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if got, want := len(consumers), 100; got != want {
			t.Fatalf("len(consumers) = %d, want %d (fallback capped at the legacy bound, one repo per row)", got, want)
		}
		if !truncated {
			t.Fatal("truncated = false, want true (the fallback read more rows than it shipped)")
		}
	})
}
