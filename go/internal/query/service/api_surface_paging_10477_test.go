// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// endpointPagingRows builds count endpoint rows in (path, endpoint_id) order,
// so a paged read must preserve the input order for the pages to be correct.
func endpointPagingRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for index := range count {
		rows = append(rows, map[string]any{
			"endpoint_id": fmt.Sprintf("endpoint-%03d", index),
			"path":        fmt.Sprintf("/v1/resource-%03d", index),
			"methods":     []any{"GET"},
		})
	}
	return rows
}

// endpointPagingGraphReader answers the repo endpoint count with total and the
// detail read with the limit/offset slice of rows, so the paged list path is
// proven against the params the production query sends, not a pre-sliced row.
func endpointPagingGraphReader(rows []map[string]any) graph.FakeWorkloadGraphReader {
	return graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "count(endpoint) AS endpoint_count"):
				return []map[string]any{{"endpoint_count": len(rows)}}, nil
			case strings.Contains(cypher, "endpoint.id AS endpoint_id"):
				limit := querycontract.IntVal(params, "limit")
				offset := querycontract.IntVal(params, "offset")
				if offset < 0 {
					offset = 0
				}
				if offset > len(rows) {
					offset = len(rows)
				}
				end := len(rows)
				if limit > 0 && offset+limit < end {
					end = offset + limit
				}
				return append([]map[string]any(nil), rows[offset:end]...), nil
			default:
				return nil, nil
			}
		},
	}
}

// TestListServiceAPISurfaceEndpointsPagesInOrder is the #10477 Q1 RED test:
// the dedicated endpoint list must return all 60 endpoints across pages in
// ORDER BY path, endpoint_id order with total == endpoint_count == 60 and no
// next_offset on the last page.
func TestListServiceAPISurfaceEndpointsPagesInOrder(t *testing.T) {
	t.Parallel()

	rows := endpointPagingRows(60)
	reader := endpointPagingGraphReader(rows)

	var got []map[string]any
	offset := 0
	pages := 0
	for {
		page, err := ListServiceAPISurfaceEndpoints(context.Background(), reader, "repo-paged", 25, offset)
		if err != nil {
			t.Fatalf("ListServiceAPISurfaceEndpoints(offset=%d) error = %v", offset, err)
		}
		if total := querycontract.IntVal(page, "endpoint_count"); total != 60 {
			t.Fatalf("page endpoint_count = %d, want 60 (the count-query total)", total)
		}
		endpoints := querycontract.MapSliceValue(page, "endpoints")
		if len(endpoints) == 0 {
			t.Fatalf("page at offset %d returned no endpoints", offset)
		}
		got = append(got, endpoints...)
		next, hasNext := page["next_offset"]
		pages++
		if !hasNext || next == nil {
			break
		}
		nextOffset, ok := next.(int)
		if !ok {
			t.Fatalf("next_offset = %#v (%T), want int", next, next)
		}
		if nextOffset != offset+len(endpoints) {
			t.Fatalf("next_offset = %d, want %d", nextOffset, offset+len(endpoints))
		}
		offset = nextOffset
		if pages > 10 {
			t.Fatal("paging did not terminate after 10 pages")
		}
	}
	if len(got) != 60 {
		t.Fatalf("paged endpoints = %d rows, want 60", len(got))
	}
	for index, row := range got {
		if want := fmt.Sprintf("/v1/resource-%03d", index); querycontract.StringVal(row, "path") != want {
			t.Fatalf("row %d path = %q, want %q (ORDER BY path, endpoint_id)", index, querycontract.StringVal(row, "path"), want)
		}
	}
	seen := map[string]bool{}
	for _, row := range got {
		id := querycontract.StringVal(row, "path")
		if seen[id] {
			t.Fatalf("duplicate endpoint %q across pages", id)
		}
		seen[id] = true
	}
}

// TestServiceStoryDossierKeeps50RowEndpointPreview is the #10477 Q1
// compatibility half: the dossier api_surface stays a 50-row preview with the
// count total, and result_limits points at the dedicated list tool.
func TestServiceStoryDossierKeeps50RowEndpointPreview(t *testing.T) {
	t.Parallel()

	ctx := testutil.SampleServiceDossierContext()
	endpoints := make([]map[string]any, 0, 60)
	for index := range 60 {
		endpoints = append(endpoints, map[string]any{
			"path":    fmt.Sprintf("/v1/resource-%03d", index),
			"methods": []string{"GET"},
		})
	}
	surface := querycontract.MapValue(ctx, "api_surface")
	surface["endpoints"] = endpoints
	surface["endpoint_count"] = len(endpoints)
	ctx["api_surface"] = surface

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	apiSurface := querycontract.MapValue(got, "api_surface")
	if n := len(querycontract.MapSliceValue(apiSurface, "endpoints")); n != serviceStoryItemLimit {
		t.Fatalf("dossier endpoints = %d rows, want preview cap %d", n, serviceStoryItemLimit)
	}
	if !querycontract.BoolVal(apiSurface, "truncated") {
		t.Fatal("dossier api_surface.truncated = false, want true past the 50-row preview")
	}
	if total := querycontract.IntVal(apiSurface, "endpoint_count"); total != 60 {
		t.Fatalf("dossier endpoint_count = %d, want 60", total)
	}
	limits := querycontract.MapValue(got, "result_limits")
	if querycontract.StringVal(limits, "endpoint_list_tool") != "list_service_api_endpoints" {
		t.Fatalf("result_limits.endpoint_list_tool = %q, want the dedicated list tool", querycontract.StringVal(limits, "endpoint_list_tool"))
	}
	if path := querycontract.StringVal(limits, "endpoint_list_path"); !strings.Contains(path, "/api-surface/endpoints") {
		t.Fatalf("result_limits.endpoint_list_path = %q, want the paged route", path)
	}
}

// enrichWithCandidateRowsAndBound runs the real enrichment over candidateRowCount
// provisioning-candidate rows with an explicit downstream bound, so the Q2
// recovery path is proven through the production read, not a sliced fixture.
func enrichWithCandidateRowsAndBound(t *testing.T, candidateRowCount, downstreamReadLimit int) map[string]any {
	t.Helper()

	workloadContext := provisioningTruncationWorkload()
	reader := provisioningCandidateGraphReader(workloadContext, provisioningCandidateRows(candidateRowCount))
	if err := EnrichServiceQueryContextWithOptions(
		context.Background(),
		reader,
		content.FakePortContentStore{},
		workloadContext,
		QueryEnrichmentOptions{
			IncludeRelatedModuleUsage: true,
			Operation:                 "service_context",
			DownstreamReadLimit:       downstreamReadLimit,
		},
	); err != nil {
		t.Fatalf("EnrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}
	return workloadContext
}

// TestDownstreamReadLimitRecoversThirtyConsumers is the #10477 Q2 RED test: a
// 30-consumer service truncates at the default bound and names it, and one
// re-call with downstream_read_limit=30 returns all 30 with the effective
// bound echoed.
func TestDownstreamReadLimitRecoversThirtyConsumers(t *testing.T) {
	t.Parallel()

	t.Run("default call truncates and names the bound", func(t *testing.T) {
		t.Parallel()

		workloadContext := enrichWithCandidateRowsAndBound(t, 30, 0)
		if !querycontract.BoolVal(workloadContext, "dependents_truncated") {
			t.Fatal("dependents_truncated = false, want true (30 candidates past the default 25-row bound)")
		}
		if got := len(querycontract.MapSliceValue(workloadContext, "dependents")); got != querycontract.DefaultIndirectEvidenceSearchLimit {
			t.Fatalf("len(dependents) = %d, want the default bound %d", got, querycontract.DefaultIndirectEvidenceSearchLimit)
		}
		if got := querycontract.IntVal(workloadContext, "downstream_read_limit"); got != querycontract.DefaultIndirectEvidenceSearchLimit {
			t.Fatalf("downstream_read_limit = %d, want the effective default %d", got, querycontract.DefaultIndirectEvidenceSearchLimit)
		}
		limitations := querycontract.StringSliceVal(workloadContext, "limitations")
		for _, token := range []string{"dependents_truncated", "consumer_repositories_truncated", "provisioning_source_chains_truncated"} {
			found := false
			for _, limitation := range limitations {
				if limitation == token {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("limitations = %q, want it to contain %q", limitations, token)
			}
		}
		reasons := querycontract.ContextPartialReasons(workloadContext)
		found := false
		for _, reason := range reasons {
			if reason == "consumer_repositories_truncated" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("partial_reasons = %q, want the consumer token (story/context agreement)", reasons)
		}
	})

	t.Run("bound of 30 recovers all 30", func(t *testing.T) {
		t.Parallel()

		workloadContext := enrichWithCandidateRowsAndBound(t, 30, 30)
		if got := len(querycontract.MapSliceValue(workloadContext, "dependents")); got != 30 {
			t.Fatalf("len(dependents) = %d, want 30 (bound raised past the fan-out)", got)
		}
		if value, present := workloadContext["dependents_truncated"]; present {
			t.Fatalf("dependents_truncated = %#v, want absent (nothing dropped at bound 30)", value)
		}
		if got := querycontract.IntVal(workloadContext, "downstream_read_limit"); got != 30 {
			t.Fatalf("downstream_read_limit = %d, want the effective bound 30", got)
		}
	})

	t.Run("over-max bound clamps to the ceiling", func(t *testing.T) {
		t.Parallel()

		workloadContext := enrichWithCandidateRowsAndBound(t, 30, 500)
		if got := querycontract.IntVal(workloadContext, "downstream_read_limit"); got != querycontract.MaxIndirectEvidenceSearchLimit {
			t.Fatalf("downstream_read_limit = %d, want clamp to %d", got, querycontract.MaxIndirectEvidenceSearchLimit)
		}
		if got := len(querycontract.MapSliceValue(workloadContext, "dependents")); got != 30 {
			t.Fatalf("len(dependents) = %d, want 30", got)
		}
	})
}

// TestCoverageReasonNamesFamilyAndBound is the #10477 Q3 RED test: the
// coverage reason names each truncated family with the bound that fired, and
// keeps the legacy sentence when nothing truncated.
func TestCoverageReasonNamesFamilyAndBound(t *testing.T) {
	t.Parallel()

	coverageReason := func(ctx map[string]any) string {
		t.Helper()
		return querycontract.StringVal(serviceInvestigationCoverage(ctx, nil, 0, nil, []string{"service_story"}), "reason")
	}

	t.Run("downstream-only names downstream_read_limit", func(t *testing.T) {
		t.Parallel()

		reason := coverageReason(enrichWithCandidateRowsAndBound(t, 30, 0))
		for _, want := range []string{"dependents truncated at downstream_read_limit=25", "consumer_repositories truncated at downstream_read_limit=25"} {
			if !strings.Contains(reason, want) {
				t.Fatalf("coverage reason = %q, want clause %q", reason, want)
			}
		}
		if strings.Contains(reason, "evidence_file_read_limit") {
			t.Fatalf("coverage reason = %q, want no evidence-file clause (that bound did not fire)", reason)
		}
	})

	t.Run("evidence-file-only names evidence_file_read_limit", func(t *testing.T) {
		t.Parallel()

		workloadContext := provisioningTruncationWorkload()
		reader := provisioningCandidateGraphReader(workloadContext, provisioningCandidateRows(2))
		store := content.FakePortContentStore{RepoFiles: serviceEvidenceFileCorpus(serviceEvidenceFileLimit + 1)}
		if err := EnrichServiceQueryContextWithOptions(
			context.Background(),
			reader,
			store,
			workloadContext,
			QueryEnrichmentOptions{IncludeRelatedModuleUsage: true, Operation: "service_context"},
		); err != nil {
			t.Fatalf("EnrichServiceQueryContextWithOptions() error = %v, want nil", err)
		}
		if !querycontract.BoolVal(workloadContext, "consumer_repositories_truncated") {
			t.Fatal("consumer_repositories_truncated = false, want true (evidence file list hit its bound)")
		}
		if querycontract.BoolVal(workloadContext, "dependents_truncated") {
			t.Fatal("dependents_truncated = true, want false (2 candidates never hit the downstream bound)")
		}
		reason := coverageReason(workloadContext)
		if want := "consumer_repositories truncated at evidence_file_read_limit=5000"; !strings.Contains(reason, want) {
			t.Fatalf("coverage reason = %q, want clause %q", reason, want)
		}
		if strings.Contains(reason, "downstream_read_limit=") {
			t.Fatalf("coverage reason = %q, want no downstream clause (that bound did not fire)", reason)
		}
	})

	t.Run("untruncated keeps the legacy sentence", func(t *testing.T) {
		t.Parallel()

		reason := coverageReason(enrichWithCandidateRowsAndBound(t, 2, 0))
		if strings.Contains(reason, "truncated at") {
			t.Fatalf("coverage reason = %q, want the legacy sentence with no family clause", reason)
		}
	})
}
