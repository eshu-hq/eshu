// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// pagingEndpointRows returns count endpoint rows in the detail query's ORDER
// BY order, so the fake can window them with the params the production query
// sends.
func pagingEndpointRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := range count {
		rows = append(rows, map[string]any{
			"endpoint_id":   fmt.Sprintf("endpoint-%03d", i),
			"path":          fmt.Sprintf("/v1/resource-%03d", i),
			"methods":       []any{"GET"},
			"source_kinds":  []any{"openapi"},
			"source_paths":  []any{"openapi.yaml"},
			"workload_id":   "workload:service-edge-api",
			"workload_name": "service-edge-api",
		})
	}
	return rows
}

// pagingServiceReader answers service resolution like the story readback
// harness, the endpoint count with the fixture total, and the endpoint
// detail with the limit/offset window. candidateCount seeds the
// provisioning-candidate read; 0 leaves the downstream lists clean.
func pagingServiceReader(t *testing.T, endpointRows []map[string]any, candidateCount int) fakeWorkloadGraphReader {
	t.Helper()
	candidates := make([]map[string]any, 0, candidateCount)
	for i := range candidateCount {
		candidates = append(candidates, map[string]any{
			"repo_id":             fmt.Sprintf("repository:consumer-%02d", i),
			"repo_name":           fmt.Sprintf("consumer-%02d", i),
			"relationship_type":   "USES_MODULE",
			"relationship_reason": "terraform_module_source_path",
		})
	}
	return fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo-service-edge-api", "repo_name": "service-edge-api"}}, nil
			case strings.Contains(cypher, "w.name = $service_name"):
				return []map[string]any{{"id": "workload:service-edge-api", "name": "service-edge-api", "kind": "service", "repo_id": "repo-service-edge-api"}}, nil
			case strings.Contains(cypher, "r.id IN $repo_ids"):
				return []map[string]any{{"repo_id": "repo-service-edge-api", "repo_name": "service-edge-api"}}, nil
			case strings.Contains(cypher, "count(endpoint) AS endpoint_count"):
				return []map[string]any{{"endpoint_count": len(endpointRows)}}, nil
			case strings.Contains(cypher, "endpoint.id AS endpoint_id"):
				limit, _ := params["limit"].(int)
				offset, _ := params["offset"].(int)
				if offset < 0 {
					offset = 0
				}
				if offset > len(endpointRows) {
					offset = len(endpointRows)
				}
				end := len(endpointRows)
				if limit > 0 && offset+limit < end {
					end = offset + limit
				}
				return append([]map[string]any(nil), endpointRows[offset:end]...), nil
			case strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM"):
				return candidates, nil
			default:
				return nil, nil
			}
		},
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			switch {
			case strings.Contains(cypher, "w.id = $workload_id"):
				return map[string]any{"id": "workload:service-edge-api", "name": "service-edge-api", "kind": "service", "repo_id": "repo-service-edge-api"}, nil
			case strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})"):
				return map[string]any{"repo_id": "repo-service-edge-api", "repo_name": "service-edge-api"}, nil
			default:
				return nil, nil
			}
		},
	}
}

func servePagingService(t *testing.T, reader fakeWorkloadGraphReader, target string) (int, map[string]any) {
	t.Helper()
	handler := &EntityHandler{
		Neo4j:   reader.delegate(),
		Content: fakePortContentStore{},
		Profile: querycontract.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	if strings.Contains(target, "/api/v0/services/") {
		segments := strings.Split(target, "/")
		name := segments[4]
		if mark := strings.Index(name, "?"); mark >= 0 {
			name = name[:mark]
		}
		req.SetPathValue("service_name", name)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET %s: unmarshal: %v (body %s)", target, err, w.Body.String())
	}
	return w.Code, body
}

func pagingEnvelopeData(t *testing.T, target string, body map[string]any) map[string]any {
	t.Helper()
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("GET %s: envelope data type = %T, want object (body %v)", target, body["data"], body)
	}
	return data
}

// TestGetServiceAPISurfaceEndpointsPagesSixty is the #10477 Q1 HTTP RED test:
// the dedicated route pages all 60 endpoints with total == endpoint_count and
// no next_offset on the last page.
func TestGetServiceAPISurfaceEndpointsPagesSixty(t *testing.T) {
	t.Parallel()

	reader := pagingServiceReader(t, pagingEndpointRows(60), 0)

	var got []map[string]any
	offset := 0
	for {
		code, body := servePagingService(t, reader, fmt.Sprintf("/api/v0/services/service-edge-api/api-surface/endpoints?limit=25&offset=%d", offset))
		if code != http.StatusOK {
			t.Fatalf("GET endpoints offset=%d: status = %d, want 200 (body %v)", offset, code, body)
		}
		data := pagingEnvelopeData(t, "endpoints", body)
		if total := querycontract.IntVal(data, "endpoint_count"); total != 60 {
			t.Fatalf("offset %d: endpoint_count = %d, want 60", offset, total)
		}
		got = append(got, querycontract.MapSliceValue(data, "endpoints")...)
		next, present := data["next_offset"]
		if !present || next == nil {
			break
		}
		offset = int(next.(float64))
		if len(got) > 60 {
			t.Fatal("paging did not terminate")
		}
	}
	if len(got) != 60 {
		t.Fatalf("paged endpoints = %d rows, want 60", len(got))
	}
	for i, row := range got {
		if want := fmt.Sprintf("/v1/resource-%03d", i); querycontract.StringVal(row, "path") != want {
			t.Fatalf("row %d path = %q, want %q", i, querycontract.StringVal(row, "path"), want)
		}
	}
}

// TestGetServiceAPISurfaceEndpointsEmptyPage pins the empty-set contract: a
// resolved service with no endpoints answers 200 with total 0 and no
// next_offset, not 404.
func TestGetServiceAPISurfaceEndpointsEmptyPage(t *testing.T) {
	t.Parallel()

	reader := pagingServiceReader(t, pagingEndpointRows(0), 0)
	code, body := servePagingService(t, reader, "/api/v0/services/service-edge-api/api-surface/endpoints")
	if code != http.StatusOK {
		t.Fatalf("GET endpoints empty: status = %d, want 200 (body %v)", code, body)
	}
	data := pagingEnvelopeData(t, "endpoints", body)
	if total := querycontract.IntVal(data, "endpoint_count"); total != 0 {
		t.Fatalf("endpoint_count = %d, want 0", total)
	}
	if len(querycontract.MapSliceValue(data, "endpoints")) != 0 {
		t.Fatalf("endpoints non-empty on an empty surface")
	}
	if next, present := data["next_offset"]; present && next != nil {
		t.Fatalf("next_offset = %v, want absent on a complete page", next)
	}
}

// TestGetServiceAPISurfaceEndpointsRejectsBadPaging pins the 400s for the
// new route's limit/offset parameters.
func TestGetServiceAPISurfaceEndpointsRejectsBadPaging(t *testing.T) {
	t.Parallel()

	reader := pagingServiceReader(t, pagingEndpointRows(60), 0)
	for _, target := range []string{
		"/api/v0/services/service-edge-api/api-surface/endpoints?limit=abc",
		"/api/v0/services/service-edge-api/api-surface/endpoints?limit=0",
		"/api/v0/services/service-edge-api/api-surface/endpoints?limit=-1",
		"/api/v0/services/service-edge-api/api-surface/endpoints?offset=abc",
		"/api/v0/services/service-edge-api/api-surface/endpoints?offset=-1",
	} {
		if code, _ := servePagingService(t, reader, target); code != http.StatusBadRequest {
			t.Fatalf("GET %s: status = %d, want 400", target, code)
		}
	}
}

// TestServiceDownstreamReadLimitParam pins the story/context 400s and the
// over-max clamp echo for downstream_read_limit (#10477 Q2).
func TestServiceDownstreamReadLimitParam(t *testing.T) {
	t.Parallel()

	reader := pagingServiceReader(t, pagingEndpointRows(2), 2)
	for _, target := range []string{
		"/api/v0/services/service-edge-api/story?downstream_read_limit=abc",
		"/api/v0/services/service-edge-api/story?downstream_read_limit=-1",
		"/api/v0/services/service-edge-api/context?downstream_read_limit=abc",
		"/api/v0/services/service-edge-api/context?downstream_read_limit=-5",
	} {
		if code, _ := servePagingService(t, reader, target); code != http.StatusBadRequest {
			t.Fatalf("GET %s: status = %d, want 400", target, code)
		}
	}

	code, body := servePagingService(t, reader, "/api/v0/services/service-edge-api/story?downstream_read_limit=500")
	if code != http.StatusOK {
		t.Fatalf("GET story clamped: status = %d, want 200 (body %v)", code, body)
	}
	limits := querycontract.MapValue(pagingEnvelopeData(t, "story", body), "result_limits")
	if got := querycontract.IntVal(limits, "downstream_read_limit"); got != querycontract.MaxIndirectEvidenceSearchLimit {
		t.Fatalf("result_limits.downstream_read_limit = %d, want clamp to %d", got, querycontract.MaxIndirectEvidenceSearchLimit)
	}
}

// TestServiceStoryContextConsumerTruncationAgrees is the #10477 agreement RED
// test through both GET handlers: a consumer-truncated service reports
// truncated on the story and names the consumer token in the context
// partial_reasons, with result_limits.truncated on both.
func TestServiceStoryContextConsumerTruncationAgrees(t *testing.T) {
	t.Parallel()

	reader := pagingServiceReader(t, pagingEndpointRows(2), 30)

	code, storyBody := servePagingService(t, reader, "/api/v0/services/service-edge-api/story")
	if code != http.StatusOK {
		t.Fatalf("GET story: status = %d, want 200 (body %v)", code, storyBody)
	}
	story := pagingEnvelopeData(t, "story", storyBody)
	storyConsumers := querycontract.MapValue(story, "downstream_consumers")
	if !querycontract.BoolVal(storyConsumers, "truncated") {
		t.Fatal("story downstream_consumers.truncated = false, want true (30 candidates past the 25-row bound)")
	}
	storyLimits := querycontract.MapValue(story, "result_limits")
	if !querycontract.BoolVal(storyLimits, "truncated") {
		t.Fatal("story result_limits.truncated = false, want true")
	}

	code, contextBody := servePagingService(t, reader, "/api/v0/services/service-edge-api/context")
	if code != http.StatusOK {
		t.Fatalf("GET context: status = %d, want 200 (body %v)", code, contextBody)
	}
	ctx := pagingEnvelopeData(t, "context", contextBody)
	contextLimits := querycontract.MapValue(ctx, "result_limits")
	if !querycontract.BoolVal(contextLimits, "truncated") {
		t.Fatal("context result_limits.truncated = false, want true")
	}
	partial := querycontract.StringSliceVal(ctx, "partial_reasons")
	found := false
	for _, reason := range partial {
		if reason == "consumer_repositories_truncated" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("context partial_reasons = %q, want the consumer token", partial)
	}
}
