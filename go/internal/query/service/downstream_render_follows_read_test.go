// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestDownstreamRenderFollowsExplicitReadLimit is the follow-up regression
// for eshu#10477 (live QA evidence on a 52-consumer service): with an explicit
// downstream_read_limit=100 the enrichment reads 52 consumer repositories,
// but the story rendered only serviceStoryItemLimit (50), leaving 2 read
// rows unreachable with no cursor. The render cap must follow the
// explicitly requested read on the downstream lists: 52 read renders 52.
func TestDownstreamRenderFollowsExplicitReadLimit(t *testing.T) {
	t.Parallel()

	workloadContext := func() map[string]any {
		consumers := make([]map[string]any, 0, 52)
		for i := 0; i < 52; i++ {
			consumers = append(consumers, map[string]any{
				"repo_id":   fmt.Sprintf("repository:consumer-%02d", i),
				"repo_name": fmt.Sprintf("consumer-%02d", i),
			})
		}
		return map[string]any{
			"name":                  "render-follows-read",
			"dependents":            []map[string]any{{"repo_id": "repository:dependent-1"}},
			"consumer_repositories": consumers,
			"downstream_read_limit": 100,
		}
	}

	t.Run("downstream consumers render every explicitly requested row", func(t *testing.T) {
		t.Parallel()

		got := BuildServiceDownstreamConsumers(workloadContext())
		if gotCount := querycontract.IntVal(got, "content_consumer_count"); gotCount != 52 {
			t.Fatalf("content_consumer_count = %d, want 52 (the rows the read returned)", gotCount)
		}
		rows := querycontract.MapSliceValue(got, "content_consumers")
		if len(rows) != 52 {
			t.Fatalf("len(content_consumers) = %d, want 52 (render must follow the explicit read, not the 50-row preview cap)", len(rows))
		}
		if querycontract.BoolVal(got, "truncated") {
			t.Fatalf("truncated = true, want false (52 rows fit the requested 100-row read)")
		}
	})

	t.Run("raw consumer copy renders every explicitly requested row", func(t *testing.T) {
		t.Parallel()

		buildCtx := newServiceStoryBuildContext(workloadContext())
		response := map[string]any{}
		enrichServiceStoryDossierResponseWithContext(response, buildCtx)
		rows := querycontract.MapSliceValue(response, "consumer_repositories")
		if len(rows) != 52 {
			t.Fatalf("len(response consumer_repositories) = %d, want 52", len(rows))
		}
		rawLimit := querycontract.MapValue(querycontract.MapValue(response, "raw_context_limits"), "consumer_repositories")
		if got := querycontract.IntVal(rawLimit, "count"); got != 52 {
			t.Fatalf("raw_context_limits.consumer_repositories.count = %d, want 52", got)
		}
		if got := querycontract.IntVal(rawLimit, "limit"); got != 100 {
			t.Fatalf("raw_context_limits.consumer_repositories.limit = %d, want the 100-row effective read bound", got)
		}
		if querycontract.BoolVal(rawLimit, "truncated") {
			t.Fatalf("raw_context_limits.consumer_repositories.truncated = true, want false (nothing cut)")
		}
	})

	t.Run("result limits name the effective render bound", func(t *testing.T) {
		t.Parallel()

		buildCtx := newServiceStoryBuildContext(workloadContext())
		limits := buildServiceResultLimitsWithContext(buildCtx)
		if got := querycontract.IntVal(limits, "downstream_render_limit"); got != 100 {
			t.Fatalf("result_limits.downstream_render_limit = %d, want 100 (the bound the render actually applied)", got)
		}
		if got := querycontract.IntVal(limits, "downstream_read_limit"); got != 100 {
			t.Fatalf("result_limits.downstream_read_limit = %d, want the 100-row effective read bound", got)
		}
		if querycontract.BoolVal(limits, "truncated") {
			t.Fatalf("result_limits.truncated = true, want false (52 downstream rows fit the 100-row render)")
		}
	})

	t.Run("default request keeps the 50-row preview", func(t *testing.T) {
		t.Parallel()

		ctx := workloadContext()
		delete(ctx, "downstream_read_limit")
		got := BuildServiceDownstreamConsumers(ctx)
		rows := querycontract.MapSliceValue(got, "content_consumers")
		if len(rows) != 50 {
			t.Fatalf("len(content_consumers) = %d, want 50 (no explicit read: preview cap unchanged)", len(rows))
		}
	})
}

// TestDownstreamRenderBoundaryRows pins the render cap at the exact
// boundaries: at or below the effective bound every row ships untruncated,
// one row past it ships the bound with truncated true. The dependents
// family follows the same rule.
func TestDownstreamRenderBoundaryRows(t *testing.T) {
	t.Parallel()

	rows := func(n int) []map[string]any {
		out := make([]map[string]any, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{"repo_id": fmt.Sprintf("repository:consumer-%03d", i)})
		}
		return out
	}
	dependentRows := func(n int) []map[string]any {
		out := make([]map[string]any, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{"repo_id": fmt.Sprintf("repository:dependent-%03d", i)})
		}
		return out
	}

	for _, tc := range []struct {
		name          string
		rows          int
		bound         int
		wantRendered  int
		wantTruncated bool
		wantRenderCap int
	}{
		{"exactly at preview with explicit read", 50, 100, 50, false, 100},
		{"one past preview renders", 51, 100, 51, false, 100},
		{"exactly at max read renders all", 100, 100, 100, false, 100},
		{"one past max read truncates", 101, 100, 100, true, 100},
		{"no explicit read keeps preview", 60, 0, 50, true, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := map[string]any{
				"name":                  "render-boundary",
				"dependents":            dependentRows(1),
				"consumer_repositories": rows(tc.rows),
			}
			if tc.bound > 0 {
				ctx["downstream_read_limit"] = tc.bound
			}
			got := BuildServiceDownstreamConsumers(ctx)
			rendered := querycontract.MapSliceValue(got, "content_consumers")
			if len(rendered) != tc.wantRendered {
				t.Fatalf("len(content_consumers) = %d, want %d (rows=%d bound=%d)", len(rendered), tc.wantRendered, tc.rows, tc.bound)
			}
			if trunc := querycontract.BoolVal(got, "truncated"); trunc != tc.wantTruncated {
				t.Fatalf("truncated = %v, want %v (rows=%d bound=%d)", trunc, tc.wantTruncated, tc.rows, tc.bound)
			}
			buildCtx := newServiceStoryBuildContext(ctx)
			limits := buildServiceResultLimitsWithContext(buildCtx)
			if got := querycontract.IntVal(limits, "downstream_render_limit"); got != tc.wantRenderCap {
				t.Fatalf("result_limits.downstream_render_limit = %d, want %d", got, tc.wantRenderCap)
			}
		})
	}

	t.Run("dependents follow the same rule", func(t *testing.T) {
		t.Parallel()

		ctx := map[string]any{
			"name":                  "render-boundary-dependents",
			"dependents":            dependentRows(52),
			"consumer_repositories": rows(3),
			"downstream_read_limit": 100,
		}
		got := BuildServiceDownstreamConsumers(ctx)
		if rendered := querycontract.MapSliceValue(got, "graph_dependents"); len(rendered) != 52 {
			t.Fatalf("len(graph_dependents) = %d, want 52", len(rendered))
		}
		if querycontract.BoolVal(got, "truncated") {
			t.Fatalf("truncated = true, want false (52 dependents fit the requested 100-row read)")
		}
	})
}
