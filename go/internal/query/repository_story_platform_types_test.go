// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

// TestQueryRepositoryStoryPlatformTypesDistinctPreventsStarvation guards the
// #5764 follow-up correctness bound: queryRepositoryStoryPlatformTypes's
// Cypher emits one row per WorkloadInstance path (two extra hops past the
// repository anchor: INSTANCE_OF, RUNS_ON), not one row per platform. Once
// queryRepositoryStoryStringRows bounds every caller with LIMIT
// repositoryStoryStringRowLimit, a raw-row LIMIT on that path shape lets a
// repository with many instances of one platform (alphabetically first)
// starve a different, real platform out of the story entirely -- a WRONG
// story, not merely a truncated one.
//
// The fake reader below encodes the graph engine's actual DISTINCT-before-
// LIMIT execution order: it applies deduplication only when the observed
// Cypher text contains "DISTINCT", exactly mirroring what a mutation that
// drops the production RETURN DISTINCT clause would do. This makes the test
// mutation-provable against the real production query string, not a synthetic
// toggle.
func TestQueryRepositoryStoryPlatformTypesDistinctPreventsStarvation(t *testing.T) {
	t.Parallel()

	// 510 "aws" path rows followed by 2 "gcp" path rows: raw declaration
	// order matters here because a plain LIMIT (no DISTINCT) takes the first
	// repositoryStoryStringRowLimit (500) rows verbatim, which never reaches
	// the "gcp" rows appended after row 510.
	rows := make([]map[string]any, 0, 512)
	for i := 0; i < 510; i++ {
		rows = append(rows, map[string]any{"platform_type": "aws"})
	}
	rows = append(
		rows,
		map[string]any{"platform_type": "gcp"},
		map[string]any{"platform_type": "gcp"},
	)

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "RUNS_ON") {
				return nil, nil
			}
			limit := IntVal(params, "limit")
			if strings.Contains(cypher, "RETURN DISTINCT") {
				seen := make(map[string]struct{}, len(rows))
				distinct := make([]map[string]any, 0, len(rows))
				for _, row := range rows {
					value := StringVal(row, "platform_type")
					if _, ok := seen[value]; ok {
						continue
					}
					seen[value] = struct{}{}
					distinct = append(distinct, row)
				}
				if len(distinct) > limit {
					distinct = distinct[:limit]
				}
				return distinct, nil
			}
			if len(rows) > limit {
				return rows[:limit], nil
			}
			return rows, nil
		},
	}

	got, _, err := queryRepositoryStoryPlatformTypes(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil)
	if err != nil {
		t.Fatalf("queryRepositoryStoryPlatformTypes() error = %v, want nil", err)
	}

	wantPlatforms := map[string]bool{"aws": false, "gcp": false}
	for _, platform := range got {
		if _, ok := wantPlatforms[platform]; ok {
			wantPlatforms[platform] = true
		}
	}
	for platform, seen := range wantPlatforms {
		if !seen {
			t.Fatalf("queryRepositoryStoryPlatformTypes() = %v, missing distinct platform %q (starved by raw-row LIMIT)", got, platform)
		}
	}
}

// TestQueryRepositoryStoryWorkloadNamesDistinctPreventsStarvation extends the
// #5764 DISTINCT-starvation guard above to queryRepositoryStoryWorkloadNames
// (P1 review follow-up): two Workload nodes can share a name, so
// queryRepositoryStoryWorkloadNames's Cypher emits duplicate raw rows just
// like the pre-fix platform_types path did. Before RETURN DISTINCT was added
// to this query, queryRepositoryStoryStringRows's own seen-map dedup ran
// AFTER the LIMIT/truncation cap, so a repository with many duplicate-name
// Workload rows (alphabetically first) could starve a different, real
// workload name out of the story entirely -- a WRONG story, not merely a
// truncated one.
//
// The fake reader below encodes the graph engine's actual DISTINCT-before-
// LIMIT execution order: it applies deduplication only when the observed
// Cypher text contains "DISTINCT", exactly mirroring what a mutation that
// drops the production RETURN DISTINCT clause would do. This makes the test
// mutation-provable against the real production query string, not a synthetic
// toggle.
func TestQueryRepositoryStoryWorkloadNamesDistinctPreventsStarvation(t *testing.T) {
	t.Parallel()

	// 510 "checkout" path rows followed by 2 "payments" path rows: raw
	// declaration order matters here because a plain LIMIT (no DISTINCT)
	// takes the first repositoryStoryStringRowLimit (500) rows verbatim,
	// which never reaches the "payments" rows appended after row 510.
	rows := make([]map[string]any, 0, 512)
	for i := 0; i < 510; i++ {
		rows = append(rows, map[string]any{"workload_name": "checkout"})
	}
	rows = append(
		rows,
		map[string]any{"workload_name": "payments"},
		map[string]any{"workload_name": "payments"},
	)

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "DEFINES") {
				return nil, nil
			}
			limit := IntVal(params, "limit")
			if strings.Contains(cypher, "RETURN DISTINCT") {
				seen := make(map[string]struct{}, len(rows))
				distinct := make([]map[string]any, 0, len(rows))
				for _, row := range rows {
					value := StringVal(row, "workload_name")
					if _, ok := seen[value]; ok {
						continue
					}
					seen[value] = struct{}{}
					distinct = append(distinct, row)
				}
				if len(distinct) > limit {
					distinct = distinct[:limit]
				}
				return distinct, nil
			}
			if len(rows) > limit {
				return rows[:limit], nil
			}
			return rows, nil
		},
	}

	got, _, err := queryRepositoryStoryWorkloadNames(t.Context(), reader, map[string]any{"repo_id": "repo-1"}, nil, nil)
	if err != nil {
		t.Fatalf("queryRepositoryStoryWorkloadNames() error = %v, want nil", err)
	}

	wantWorkloads := map[string]bool{"checkout": false, "payments": false}
	for _, workload := range got {
		if _, ok := wantWorkloads[workload]; ok {
			wantWorkloads[workload] = true
		}
	}
	for workload, seen := range wantWorkloads {
		if !seen {
			t.Fatalf("queryRepositoryStoryWorkloadNames() = %v, missing distinct workload %q (starved by raw-row LIMIT)", got, workload)
		}
	}
}
