// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// listTestGraphReader serves candidate rows shaped like
// provisioningCandidateRows: one row per distinct repo.
func listTestGraphReader(rows int) graph.FakeGraphReader {
	out := make([]map[string]any, 0, rows)
	for i := range rows {
		out = append(out, map[string]any{
			"repo_id":             fmt.Sprintf("repository:graph-%02d", i),
			"repo_name":           fmt.Sprintf("graph-%02d", i),
			"relationship_type":   "USES_MODULE",
			"relationship_reason": "terraform_module_source_path",
		})
	}
	return graph.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return out, nil
		},
	}
}

// listTestContentStore serves exactly n distinct consumer repos from the
// indexed service-name path regardless of the requested limit, so paging
// tests control the merged set size exactly.
type listTestContentStore struct {
	content.FakePortContentStore
	total int
}

func (s listTestContentStore) SearchFileReferenceAnyRepo(_ context.Context, kind, value string, limit int) ([]querycontract.FileContent, bool, error) {
	if kind != "service_name" || limit <= 0 {
		return nil, true, nil
	}
	n := s.total
	if n > limit {
		n = limit
	}
	rows := make([]querycontract.FileContent, 0, n)
	for i := range n {
		rows = append(rows, querycontract.FileContent{
			RepoID:       fmt.Sprintf("repository:content-%02d", i),
			RelativePath: fmt.Sprintf("deploy/values-%02d.yaml", i),
		})
	}
	return rows, true, nil
}

func listTestAccess(granted ...string) querycontract.RepositoryAccessFilter {
	if granted == nil {
		return querycontract.RepositoryAccessFilter{AllScopes: true}
	}
	return querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: granted}
}

// TestListServiceDownstreamConsumersPagesUnionExactlyOnce drives the new
// paged surface end to end: 20 graph repos + 150 content repos merge, sort by
// (repository, repo_id), and page union reads every repo exactly once with a
// terminal page that carries no cursor.
func TestListServiceDownstreamConsumersPagesUnionExactlyOnce(t *testing.T) {
	t.Parallel()

	graphReader := listTestGraphReader(20)
	store := listTestContentStore{total: 150}

	var (
		seen     = map[string]int{}
		cursor   *ConsumerListCursor
		pages    int
		lastHad  bool
		consumed int
	)
	for {
		page, err := ListServiceDownstreamConsumers(
			context.Background(), graphReader, store,
			"repository:svc", "svc",
			listTestAccess(), cursor, 25,
		)
		if err != nil {
			t.Fatalf("ListServiceDownstreamConsumers() error = %v, want nil", err)
		}
		pages++
		for _, row := range page.Consumers {
			id := querycontract.StringVal(row, "repo_id")
			seen[id]++
			consumed++
		}
		if !page.HasNext {
			lastHad = page.NextCursor == nil
			break
		}
		if page.NextCursor == nil {
			t.Fatal("HasNext is true with no next cursor, want a cursor naming the last row")
		}
		cursor = page.NextCursor
		if pages > 20 {
			t.Fatal("paging did not terminate within 20 pages")
		}
	}
	// 20 graph + 150 content repos, disjoint id spaces.
	if got, want := len(seen), 170; got != want {
		t.Fatalf("distinct repos paged = %d, want %d (union exactly once)", got, want)
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("repo %s served %d times, want exactly once", id, count)
		}
	}
	if consumed != 170 {
		t.Fatalf("total rows served = %d, want 170", consumed)
	}
	if !lastHad {
		t.Fatal("terminal page carries a next cursor, want none")
	}
	if pages != 7 {
		t.Fatalf("pages = %d, want 7 (170 rows at 25 per page)", pages)
	}
}

// TestListServiceDownstreamConsumersGrantFiltersBeforePaging proves a scoped
// caller never sees an ungranted repo: the filter applies to the merged set
// before the keyset drop and the page cut, so cross-tenant content matches
// are absent from every page.
func TestListServiceDownstreamConsumersGrantFiltersBeforePaging(t *testing.T) {
	t.Parallel()

	// "repository:foreign" is content-only (no graph edge) and outside the grant.
	access := listTestAccess("repository:graph-00", "repository:content-00")
	page, err := ListServiceDownstreamConsumers(
		context.Background(), listTestGraphReader(2), listTestContentStore{total: 3},
		"repository:svc", "svc",
		access, nil, 50,
	)
	if err != nil {
		t.Fatalf("ListServiceDownstreamConsumers() error = %v, want nil", err)
	}
	for _, row := range page.Consumers {
		id := querycontract.StringVal(row, "repo_id")
		if id != "repository:graph-00" && id != "repository:content-00" {
			t.Fatalf("served ungranted repo %q to a scoped caller", id)
		}
	}
	if got, want := len(page.Consumers), 2; got != want {
		t.Fatalf("len(consumers) = %d, want %d (only granted repos)", got, want)
	}
	if page.Truncated {
		t.Fatal("truncated = true, want false (the granted set fits and no bound fired)")
	}
}

// TestListServiceDownstreamConsumersTerminalPageIsHonest proves an empty tail
// is a complete empty page, not a 404: the cursor past the end returns no
// rows, no cursor, and truncated=false when nothing fired.
func TestListServiceDownstreamConsumersTerminalPageIsHonest(t *testing.T) {
	t.Parallel()

	graphReader := listTestGraphReader(3)
	store := listTestContentStore{total: 0}
	first, err := ListServiceDownstreamConsumers(
		context.Background(), graphReader, store,
		"repository:svc", "svc",
		listTestAccess(), nil, 50,
	)
	if err != nil {
		t.Fatalf("ListServiceDownstreamConsumers() error = %v, want nil", err)
	}
	if got := len(first.Consumers); got != 3 {
		t.Fatalf("len(consumers) = %d, want 3", got)
	}
	if first.HasNext {
		t.Fatal("HasNext = true with only 3 rows at page size 50, want false")
	}
	if first.NextCursor != nil {
		t.Fatal("terminal page carries a next cursor, want none")
	}
	if first.Truncated {
		t.Fatal("truncated = true, want false (nothing fired, nothing dropped)")
	}
}

// TestListServiceDownstreamConsumersReportsReadBound proves the page carries
// the page's own disclosure: the effective internal bound and the truncation
// sources that fired to produce it.
func TestListServiceDownstreamConsumersReportsReadBound(t *testing.T) {
	t.Parallel()

	page, err := ListServiceDownstreamConsumers(
		context.Background(), listTestGraphReader(1), listTestContentStore{total: 1},
		"repository:svc", "svc",
		listTestAccess(), nil, 50,
	)
	if err != nil {
		t.Fatalf("ListServiceDownstreamConsumers() error = %v, want nil", err)
	}
	if page.EffectiveBound <= 0 {
		t.Fatalf("EffectiveBound = %d, want the positive internal read bound", page.EffectiveBound)
	}
	if !page.LowerBound {
		t.Fatal("LowerBound = false, want true (no exact-total claim on a bounded merged read)")
	}
}
