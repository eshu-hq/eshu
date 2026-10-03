// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"fmt"
	"slices"
	"testing"
)

func budgetRows(key string, n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{"id": fmt.Sprintf("%s-%03d", key, i)})
	}
	return rows
}

// budgetContext builds a context payload whose evidence lists and API surface
// all sit past ContextStoryItemLimit, with the overview carrying its second
// copy of the endpoint rows the way buildServiceDeploymentOverviewWithContext
// does.
func budgetContext(rows int) map[string]any {
	endpoints := budgetRows("endpoint", rows)
	return map[string]any{
		"api_surface": map[string]any{"endpoint_count": rows + 20, "endpoints": endpoints},
		"deployment_overview": map[string]any{
			"instance_count": 1,
			"api_surface":    map[string]any{"endpoint_count": rows + 20, "endpoints": endpoints},
		},
		"deployment_evidence": map[string]any{
			"artifact_count":      rows,
			"artifacts":           budgetRows("artifact", rows),
			"delivery_paths":      budgetRows("path", rows),
			"delivery_workflows":  budgetRows("workflow", rows),
			"shared_config_paths": budgetRows("config", rows),
			"tool_families":       []string{"argocd"},
		},
	}
}

// TestWorkloadContextResultLimitsCapsEvidenceAndAPISurfaceRows proves the
// deployment-evidence row lists and the API surface endpoints are cut to the
// row limit, with the pre-cut totals reported and every cut named (#7129).
func TestWorkloadContextResultLimitsCapsEvidenceAndAPISurfaceRows(t *testing.T) {
	t.Parallel()

	const total = 120
	ctx := budgetContext(total)

	limits := WorkloadContextResultLimits(ctx, "workload:svc", "context")

	evidence := MapValue(ctx, "deployment_evidence")
	for _, key := range []string{"artifacts", "delivery_paths", "delivery_workflows", "shared_config_paths"} {
		if got, want := len(MapSliceValue(evidence, key)), ContextStoryItemLimit; got != want {
			t.Fatalf("deployment_evidence.%s len = %d, want %d", key, got, want)
		}
		raw := MapValue(MapValue(evidence, "raw_limits"), key)
		if got := IntVal(raw, "count"); got != total {
			t.Fatalf("raw_limits.%s.count = %d, want %d (total before the cut)", key, got, total)
		}
		if !BoolVal(raw, "truncated") || IntVal(raw, "limit") != ContextStoryItemLimit {
			t.Fatalf("raw_limits.%s = %#v, want truncated at %d", key, raw, ContextStoryItemLimit)
		}
	}
	if !BoolVal(evidence, "artifacts_truncated") {
		t.Fatal("deployment_evidence.artifacts_truncated = false next to a cut list, want true")
	}
	if got := IntVal(evidence, "artifact_count"); got != total {
		t.Fatalf("deployment_evidence.artifact_count = %d, want %d (the total is not rewritten)", got, total)
	}
	surface := MapValue(ctx, "api_surface")
	if got, want := len(MapSliceValue(surface, "endpoints")), ContextStoryItemLimit; got != want {
		t.Fatalf("api_surface.endpoints len = %d, want %d", got, want)
	}
	if !BoolVal(surface, "detail_truncated") || IntVal(surface, "detail_limit") != ContextStoryItemLimit {
		t.Fatalf("api_surface detail markers = %#v, want truncated at %d", surface, ContextStoryItemLimit)
	}
	if got, want := IntVal(surface, "endpoint_count"), total+20; got != want {
		t.Fatalf("api_surface.endpoint_count = %d, want %d", got, want)
	}
	if got := IntVal(limits, "artifact_count"); got != total {
		t.Fatalf("result_limits.artifact_count = %d, want %d", got, total)
	}
	if !BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = false next to cut lists, want true")
	}
	reasons := ContextPartialReasons(ctx)
	for _, want := range []string{
		"api_surface_endpoints_truncated",
		"deployment_evidence_artifacts_truncated",
		"deployment_evidence_delivery_paths_truncated",
		"deployment_evidence_delivery_workflows_truncated",
		"deployment_evidence_shared_config_paths_truncated",
	} {
		if !slices.Contains(reasons, want) {
			t.Fatalf("partial_reasons = %#v, want %q", reasons, want)
		}
	}
}

// TestWorkloadContextResultLimitsShipsEndpointRowsOnce proves the overview
// keeps its counts but not its copy of the endpoint rows, and says where the
// rows are.
func TestWorkloadContextResultLimitsShipsEndpointRowsOnce(t *testing.T) {
	t.Parallel()

	ctx := budgetContext(10)

	WorkloadContextResultLimits(ctx, "workload:svc", "context")

	overview := MapValue(ctx, "deployment_overview")
	surface := MapValue(overview, "api_surface")
	if _, has := surface["endpoints"]; has {
		t.Fatalf("deployment_overview.api_surface = %#v, want no endpoint rows", surface)
	}
	if got, want := IntVal(surface, "endpoint_count"), 30; got != want {
		t.Fatalf("deployment_overview.api_surface.endpoint_count = %d, want %d", got, want)
	}
	if got := StringVal(surface, "endpoints_shipped_at"); got != "api_surface.endpoints" {
		t.Fatalf("endpoints_shipped_at = %q, want api_surface.endpoints", got)
	}
	if got := IntVal(overview, "instance_count"); got != 1 {
		t.Fatalf("deployment_overview.instance_count = %d, want 1 (other overview fields stay)", got)
	}
	if got := len(MapSliceValue(MapValue(ctx, "api_surface"), "endpoints")); got != 10 {
		t.Fatalf("api_surface.endpoints len = %d, want 10 (the top-level rows stay)", got)
	}
}

// TestWorkloadContextResultLimitsKeepsOverviewRowsWithoutTopLevelCopy proves
// the overview rows are never dropped when no top-level row list exists to
// carry them.
func TestWorkloadContextResultLimitsKeepsOverviewRowsWithoutTopLevelCopy(t *testing.T) {
	t.Parallel()

	ctx := budgetContext(10)
	delete(ctx, "api_surface")

	WorkloadContextResultLimits(ctx, "workload:svc", "context")

	surface := MapValue(MapValue(ctx, "deployment_overview"), "api_surface")
	if got := len(MapSliceValue(surface, "endpoints")); got != 10 {
		t.Fatalf("deployment_overview.api_surface.endpoints len = %d, want 10 (the only copy)", got)
	}
}

// TestWorkloadContextResultLimitsPromotesReadTruncation proves a cut the graph
// read already made is no longer silent: artifacts_truncated and
// detail_truncated reach partial_reasons and result_limits.truncated even when
// the rows left are under the row limit.
func TestWorkloadContextResultLimitsPromotesReadTruncation(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"api_surface": map[string]any{
			"endpoint_count":   78,
			"endpoints":        budgetRows("endpoint", 10),
			"detail_truncated": true,
		},
		"deployment_evidence": map[string]any{
			"artifact_count":      10,
			"artifacts":           budgetRows("artifact", 10),
			"artifacts_truncated": true,
		},
	}

	limits := WorkloadContextResultLimits(ctx, "workload:svc", "context")

	reasons := ContextPartialReasons(ctx)
	for _, want := range []string{"api_surface_endpoints_truncated", "deployment_evidence_artifacts_truncated"} {
		if !slices.Contains(reasons, want) {
			t.Fatalf("partial_reasons = %#v, want %q", reasons, want)
		}
	}
	if !BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = false next to a truncated read, want true")
	}
	if got := len(MapSliceValue(MapValue(ctx, "deployment_evidence"), "artifacts")); got != 10 {
		t.Fatalf("artifacts len = %d, want 10 (nothing further to cut)", got)
	}
}

// TestWorkloadContextResultLimitsLeavesWithinCapEvidenceAlone proves a context
// at or under the row limit is returned whole: no marker, no limitation, and
// the evidence map is not rebuilt.
func TestWorkloadContextResultLimitsLeavesWithinCapEvidenceAlone(t *testing.T) {
	t.Parallel()

	ctx := budgetContext(ContextStoryItemLimit)
	evidence := MapValue(ctx, "deployment_evidence")

	limits := WorkloadContextResultLimits(ctx, "workload:svc", "context")

	if _, has := MapValue(ctx, "deployment_evidence")["raw_limits"]; has {
		t.Fatal("deployment_evidence.raw_limits present on a context within the row limit")
	}
	if _, has := evidence["raw_limits"]; has {
		t.Fatal("the original evidence map was written to")
	}
	if reasons := ContextPartialReasons(ctx); len(reasons) != 0 {
		t.Fatalf("partial_reasons = %#v, want none", reasons)
	}
	if BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = true on a context within the row limit")
	}
	if got := IntVal(limits, "artifact_count"); got != ContextStoryItemLimit {
		t.Fatalf("result_limits.artifact_count = %d, want %d", got, ContextStoryItemLimit)
	}
}

// TestWorkloadContextResultLimitsDoesNotMutateSharedEvidenceMap proves the cut
// is applied to a copy, so an evidence map a read model hands out to several
// callers keeps every row.
func TestWorkloadContextResultLimitsDoesNotMutateSharedEvidenceMap(t *testing.T) {
	t.Parallel()

	ctx := budgetContext(120)
	shared := MapValue(ctx, "deployment_evidence")

	WorkloadContextResultLimits(ctx, "workload:svc", "context")

	if got := len(MapSliceValue(shared, "artifacts")); got != 120 {
		t.Fatalf("shared evidence artifacts len = %d, want 120 (untouched)", got)
	}
	if _, has := shared["raw_limits"]; has {
		t.Fatal("shared evidence map gained raw_limits")
	}
}

// TestWorkloadContextResultLimitsStorySurfaceEmitsNoEvidence proves the story
// surface, which ships a narrative and neither list, reports the artifact total
// but neither cuts nor claims truncation for rows it does not emit.
func TestWorkloadContextResultLimitsStorySurfaceEmitsNoEvidence(t *testing.T) {
	t.Parallel()

	ctx := budgetContext(120)

	limits := WorkloadContextResultLimits(ctx, "workload:svc", "story")

	if got := len(MapSliceValue(MapValue(ctx, "deployment_evidence"), "artifacts")); got != 120 {
		t.Fatalf("story artifacts len = %d, want 120 (not cut)", got)
	}
	if got := len(MapSliceValue(MapValue(ctx, "api_surface"), "endpoints")); got != 120 {
		t.Fatalf("story api_surface.endpoints len = %d, want 120 (not cut)", got)
	}
	if got := IntVal(limits, "artifact_count"); got != 120 {
		t.Fatalf("result_limits.artifact_count = %d, want 120", got)
	}
	if reasons := ContextPartialReasons(ctx); len(reasons) != 0 {
		t.Fatalf("partial_reasons = %#v, want none on a surface that emits no such list", reasons)
	}
	if BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = true for lists the story does not emit")
	}
}

func infrastructureRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{"name": fmt.Sprintf("res-%04d", i), "kind": "Deployment", "file_path": fmt.Sprintf("deploy/overlays/prod/res-%04d.yaml", i)})
	}
	return rows
}

// TestWorkloadContextResultLimitsCapsInfrastructureRows proves the
// infrastructure list is cut to the row limit on the context surface, with the
// true total on result_limits and a reason of its own. The read bound is 5,000
// rows, so before this cap a service shipped up to 5,000 rows (#7129).
func TestWorkloadContextResultLimitsCapsInfrastructureRows(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{"infrastructure": infrastructureRows(1161)}

	limits := WorkloadContextResultLimits(ctx, "workload:svc", "context")

	if got, want := len(MapSliceValue(ctx, "infrastructure")), ContextStoryItemLimit; got != want {
		t.Fatalf("infrastructure len = %d, want %d", got, want)
	}
	if got, want := IntVal(limits, "infrastructure_count"), 1161; got != want {
		t.Fatalf("result_limits.infrastructure_count = %d, want %d (total before the cut)", got, want)
	}
	if !BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = false next to a cut list, want true")
	}
	if !slices.Contains(ContextPartialReasons(ctx), "infrastructure_rows_truncated") {
		t.Fatalf("partial_reasons = %#v, want infrastructure_rows_truncated", ContextPartialReasons(ctx))
	}
}

// TestWorkloadContextResultLimitsLeavesSmallInfrastructureAndStoryAlone proves a
// list within the limit is untouched with no reason, and the story surface,
// which emits no infrastructure list, neither cuts nor claims a cut.
func TestWorkloadContextResultLimitsLeavesSmallInfrastructureAndStoryAlone(t *testing.T) {
	t.Parallel()

	small := map[string]any{"infrastructure": infrastructureRows(ContextStoryItemLimit)}
	limits := WorkloadContextResultLimits(small, "workload:svc", "context")
	if got := len(MapSliceValue(small, "infrastructure")); got != ContextStoryItemLimit {
		t.Fatalf("within-limit infrastructure len = %d, want %d", got, ContextStoryItemLimit)
	}
	if slices.Contains(ContextPartialReasons(small), "infrastructure_rows_truncated") || BoolVal(limits, "truncated") {
		t.Fatal("a within-limit infrastructure list was reported as cut")
	}

	story := map[string]any{"infrastructure": infrastructureRows(300)}
	storyLimits := WorkloadContextResultLimits(story, "workload:svc", "story")
	if got := len(MapSliceValue(story, "infrastructure")); got != 300 {
		t.Fatalf("story infrastructure len = %d, want 300 (not cut)", got)
	}
	if got := IntVal(storyLimits, "infrastructure_count"); got != 300 || BoolVal(storyLimits, "truncated") {
		t.Fatalf("story infrastructure_count=%d truncated=%v, want 300 and false", got, BoolVal(storyLimits, "truncated"))
	}
}

func entrypointCandidateRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"candidate":      fmt.Sprintf("cfg-%04d.example.internal", i),
			"classification": "config_key",
			"relative_path":  fmt.Sprintf("config/settings/cfg-%04d.yaml", i),
			"reason":         "dotted token rejected as a config key",
		})
	}
	return rows
}

// TestWorkloadContextResultLimitsCapsEntrypointCandidates proves the
// entrypoint_candidates list is cut to the row limit on the context surface,
// with the true total on result_limits and a reason of its own. The list had no
// cap and grows with repository size: 357 rows (about 63 KB) on the largest
// ops-qa service (#7129).
func TestWorkloadContextResultLimitsCapsEntrypointCandidates(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{"entrypoint_candidates": entrypointCandidateRows(357)}

	limits := WorkloadContextResultLimits(ctx, "workload:svc", "context")

	if got, want := len(MapSliceValue(ctx, "entrypoint_candidates")), ContextStoryItemLimit; got != want {
		t.Fatalf("entrypoint_candidates len = %d, want %d", got, want)
	}
	if got, want := IntVal(limits, "entrypoint_candidate_count"), 357; got != want {
		t.Fatalf("result_limits.entrypoint_candidate_count = %d, want %d (total before the cut)", got, want)
	}
	if !BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = false next to a cut list, want true")
	}
	if !slices.Contains(ContextPartialReasons(ctx), "entrypoint_candidates_truncated") {
		t.Fatalf("partial_reasons = %#v, want entrypoint_candidates_truncated", ContextPartialReasons(ctx))
	}
	first := MapSliceValue(ctx, "entrypoint_candidates")[0]
	if got, want := SafeStr(first, "candidate"), "cfg-0000.example.internal"; got != want {
		t.Fatalf("first kept candidate = %q, want %q (the cut keeps the leading rows)", got, want)
	}
}

// TestWorkloadContextResultLimitsLeavesSmallEntrypointCandidatesAndStoryAlone
// proves a list within the limit is untouched with no reason, and the story
// surface, which ships its own bounded copy, neither cuts nor claims a cut
// while still reporting the total.
func TestWorkloadContextResultLimitsLeavesSmallEntrypointCandidatesAndStoryAlone(t *testing.T) {
	t.Parallel()

	small := map[string]any{"entrypoint_candidates": entrypointCandidateRows(ContextStoryItemLimit)}
	limits := WorkloadContextResultLimits(small, "workload:svc", "context")
	if got := len(MapSliceValue(small, "entrypoint_candidates")); got != ContextStoryItemLimit {
		t.Fatalf("within-limit entrypoint_candidates len = %d, want %d", got, ContextStoryItemLimit)
	}
	if got := IntVal(limits, "entrypoint_candidate_count"); got != ContextStoryItemLimit {
		t.Fatalf("within-limit entrypoint_candidate_count = %d, want %d", got, ContextStoryItemLimit)
	}
	if slices.Contains(ContextPartialReasons(small), "entrypoint_candidates_truncated") || BoolVal(limits, "truncated") {
		t.Fatal("a within-limit entrypoint_candidates list was reported as cut")
	}

	story := map[string]any{"entrypoint_candidates": entrypointCandidateRows(300)}
	storyLimits := WorkloadContextResultLimits(story, "workload:svc", "story")
	if got := len(MapSliceValue(story, "entrypoint_candidates")); got != 300 {
		t.Fatalf("story entrypoint_candidates len = %d, want 300 (not cut)", got)
	}
	if got := IntVal(storyLimits, "entrypoint_candidate_count"); got != 300 || BoolVal(storyLimits, "truncated") {
		t.Fatalf("story entrypoint_candidate_count=%d truncated=%v, want 300 and false", got, BoolVal(storyLimits, "truncated"))
	}
}

// TestWorkloadContextResultLimitsEntrypointCandidateCutLeavesSourceRowsAlone
// proves the cut never reaches the backing array of the caller's slice, so a
// read model that handed the same slice to another reader keeps every row.
func TestWorkloadContextResultLimitsEntrypointCandidateCutLeavesSourceRowsAlone(t *testing.T) {
	t.Parallel()

	source := entrypointCandidateRows(120)
	ctx := map[string]any{"entrypoint_candidates": source}

	WorkloadContextResultLimits(ctx, "workload:svc", "context")

	if len(source) != 120 || SafeStr(source[119], "candidate") != "cfg-0119.example.internal" {
		t.Fatalf("source slice was mutated: len=%d last=%q", len(source), SafeStr(source[len(source)-1], "candidate"))
	}
}
