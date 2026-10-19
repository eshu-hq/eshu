// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
)

// stageTimingContentStore delays every content read the code-search seams
// issue, so the tests below can assert the pg stage brackets the store
// delay. It embeds the port so only the exercised reads need bodies.
type stageTimingContentStore struct {
	querycontract.ContentStore
	delay time.Duration
}

func (s stageTimingContentStore) SearchEntitiesByName(_ context.Context, _, _, _ string, _ int) ([]querycontract.EntityContent, error) {
	time.Sleep(s.delay)
	return nil, nil
}

func (s stageTimingContentStore) SearchEntityContent(_ context.Context, _, _ string, _ int) ([]querycontract.EntityContent, error) {
	time.Sleep(s.delay)
	return nil, nil
}

func (s stageTimingContentStore) ListRepoEntitiesByIDs(_ context.Context, _ string, _ []string, _ int) ([]querycontract.EntityContent, error) {
	time.Sleep(s.delay)
	return nil, nil
}

func (s stageTimingContentStore) SearchEntityNames(_ context.Context, _ entity.EntityNameSearch) ([]querycontract.EntityContent, error) {
	time.Sleep(s.delay)
	return nil, nil
}

// TestSearchGraphEntitiesWithExactAttributesStages proves the
// repository-selected POST /api/v0/code/search seam splits its store time:
// the graph run lands on the graph stage and the content enrichment on the
// pg stage, each bracketing its injected delay (issue #7733 item 2).
func TestSearchGraphEntitiesWithExactAttributesStages(t *testing.T) {
	t.Parallel()
	ctx, stages := querycontract.WithRequestStages(context.Background())
	h := &CodeHandler{
		Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			time.Sleep(100 * time.Millisecond)
			return []map[string]any{{
				"entity_id": "entity:e1", "name": "Foo",
				"labels":     []string{"Function"},
				"file_path":  "foo.go",
				"repo_id":    "repo1",
				"repo_name":  "repo1",
				"language":   "go",
				"start_line": 1,
				"end_line":   10,
			}}, nil
		}},
		Content: stageTimingContentStore{delay: 100 * time.Millisecond},
	}
	if _, err := h.searchGraphEntitiesWithExact(ctx, "repo1", "Foo", "", 10, false); err != nil {
		t.Fatalf("searchGraphEntitiesWithExact error = %v", err)
	}
	if got := stages.Graph(); got < 50*time.Millisecond {
		t.Fatalf("graph stage = %v, want >= 50ms (100ms injected graph delay)", got)
	}
	if got := stages.PG(); got < 50*time.Millisecond {
		t.Fatalf("pg stage = %v, want >= 50ms (100ms injected enrichment delay)", got)
	}
}

// TestSearchEntityContentWithExactAttributesPGStage proves the content
// fallback seam of POST /api/v0/code/search accumulates both of its
// sequential store reads on the pg stage: two 100ms reads must sum past a
// single read's bound.
func TestSearchEntityContentWithExactAttributesPGStage(t *testing.T) {
	t.Parallel()
	ctx, stages := querycontract.WithRequestStages(context.Background())
	h := &CodeHandler{Content: stageTimingContentStore{delay: 100 * time.Millisecond}}
	if _, err := h.searchEntityContentWithExact(ctx, "repo1", "Handler", "", 10, false); err != nil {
		t.Fatalf("searchEntityContentWithExact error = %v", err)
	}
	if got := stages.PG(); got < 150*time.Millisecond {
		t.Fatalf("pg stage = %v, want >= 150ms (two sequential 100ms reads)", got)
	}
	if got := stages.Graph(); got != 0 {
		t.Fatalf("graph stage = %v, want 0 (no graph read on this seam)", got)
	}
}

// TestSearchGlobalEntityNamesAttributesPGStage proves the global (no
// repository filter) POST /api/v0/code/search seam records its
// entity-name read on the pg stage.
func TestSearchGlobalEntityNamesAttributesPGStage(t *testing.T) {
	t.Parallel()
	ctx, stages := querycontract.WithRequestStages(context.Background())
	h := &CodeHandler{Content: stageTimingContentStore{delay: 100 * time.Millisecond}}
	if _, err := h.searchGlobalEntityNames(ctx, "Foo", "", 10, false); err != nil {
		t.Fatalf("searchGlobalEntityNames error = %v", err)
	}
	if got := stages.PG(); got < 50*time.Millisecond {
		t.Fatalf("pg stage = %v, want >= 50ms (100ms injected name-search delay)", got)
	}
}
