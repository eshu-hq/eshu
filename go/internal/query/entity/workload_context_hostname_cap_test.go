// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// mcpResponseByteBudget mirrors the MCP dispatch budget
// (go/internal/mcp/dispatch_budget.go defaultToolResponseByteBudget): 256 KiB
// of serialized response, counted twice because the wire carries the payload as
// structuredContent and again as escaped resource text.
const mcpResponseByteBudget = 256 * 1024

const outlierHostnameCount = 671

// outlierHostnameHandler returns a Handler whose fakes reproduce the measured
// #7169 outlier through the real enrichment path: one workload with one qa
// runtime instance and a values file naming 671 hostnames. Enrichment turns
// the hostnames into 671 hostnames, 671 entrypoints, and (because the instance
// matches their environment) 671 network_paths.
func outlierHostnameHandler() *Handler {
	var values strings.Builder
	values.WriteString("ingress:\n  hosts:\n")
	for i := 0; i < outlierHostnameCount; i++ {
		fmt.Fprintf(&values, "    - host: svc-%03d.tenant.qa.example.test\n", i)
	}
	return &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"MATCH (w:Workload)": {
					"id":        "workload-1",
					"name":      "svc",
					"kind":      "service",
					"repo_id":   "repo-1",
					"repo_name": "svc",
					"instances": []any{map[string]any{
						"instance_id":   "inst-1",
						"platform_name": "eks-qa",
						"platform_kind": "EKS",
						"environment":   "qa",
					}},
				},
			},
			RunByMatch: map[string][]map[string]any{
				"MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)": {
					{"repo_id": "repo-1", "repo_name": "svc"},
				},
			},
		},
		Content: content.FakePortContentStore{
			RepoFiles: []querycontract.FileContent{{
				RepoID:       "repo-1",
				RelativePath: "deploy/charts/svc/values-qa.yaml",
				Content:      values.String(),
			}},
		},
	}
}

func getOutlierJSON(t *testing.T, h *Handler, path, param, value string) (map[string]any, int) {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue(param, value)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data := envelope
	if inner, ok := envelope["data"].(map[string]any); ok {
		data = inner
	}
	return data, 2 * len(w.Body.Bytes())
}

func stringSlice(t *testing.T, value any) []string {
	t.Helper()
	raw, _ := value.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

// TestGetWorkloadContextCapsHostnameEntrypointAndNetworkPathRows drives the
// real GetWorkloadContext handler with the 671-hostname outlier plus one
// matching runtime instance. network_paths carries one row per entrypoint, so
// an uncapped copy alone keeps the response over the MCP budget (#7169). The
// three lists must be cut to the row limit, the pre-cut totals reported, each
// cut named on partial_reasons, and the overview counts left at the true
// totals. Deleting the cap call from the handler turns this red.
func TestGetWorkloadContextCapsHostnameEntrypointAndNetworkPathRows(t *testing.T) {
	t.Parallel()

	data, wireBytes := getOutlierJSON(t, outlierHostnameHandler(), "/api/v0/workloads/workload-1/context", "workload_id", "workload-1")

	limit := querycontract.ContextStoryItemLimit
	for _, key := range []string{"hostnames", "entrypoints", "network_paths"} {
		rows, _ := data[key].([]any)
		if got := len(rows); got != limit {
			t.Fatalf("%s len = %d, want %d", key, got, limit)
		}
	}
	limits := querycontract.MapValue(data, "result_limits")
	for _, key := range []string{"hostname_count", "entrypoint_count", "network_path_count"} {
		if got, want := querycontract.IntVal(limits, key), outlierHostnameCount; got != want {
			t.Fatalf("result_limits.%s = %d, want %d (total before the cut)", key, got, want)
		}
	}
	if !querycontract.BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = false next to cut lists, want true")
	}
	reasons := stringSlice(t, data["partial_reasons"])
	for _, want := range []string{"hostnames_truncated", "entrypoints_truncated", "network_paths_truncated"} {
		if !slices.Contains(reasons, want) {
			t.Fatalf("partial_reasons = %v, want %q", reasons, want)
		}
	}
	overview := querycontract.MapValue(data, "deployment_overview")
	if got, want := querycontract.IntVal(overview, "network_path_count"), outlierHostnameCount; got != want {
		t.Fatalf("deployment_overview.network_path_count = %d, want %d (counts stay pre-cut)", got, want)
	}
	if wireBytes > mcpResponseByteBudget {
		t.Fatalf("context est2x = %d bytes, want <= %d", wireBytes, mcpResponseByteBudget)
	}
}

// TestGetWorkloadStoryDoesNotReportCutsForArraysItDoesNotEmit proves the story
// route, which ships a narrative and no hostname, entrypoint, or network path
// arrays, reports the totals but never names a truncation of lists it does not
// emit (#7169 F3).
func TestGetWorkloadStoryDoesNotReportCutsForArraysItDoesNotEmit(t *testing.T) {
	t.Parallel()

	data, _ := getOutlierJSON(t, outlierHostnameHandler(), "/api/v0/workloads/workload-1/story", "workload_id", "workload-1")

	limits := querycontract.MapValue(data, "result_limits")
	if got, want := querycontract.IntVal(limits, "hostname_count"), outlierHostnameCount; got != want {
		t.Fatalf("result_limits.hostname_count = %d, want %d", got, want)
	}
	// result_limits.truncated is not asserted here: with 671 hostnames the
	// consumer-repository search hits its own bound, an unrelated upstream
	// truncation the story route has always ORed in. The unit test in
	// querycontract pins truncated=false for the story surface in isolation.
	for _, reason := range stringSlice(t, data["partial_reasons"]) {
		if strings.HasSuffix(reason, "_truncated") &&
			(strings.HasPrefix(reason, "hostnames") || strings.HasPrefix(reason, "entrypoints") || strings.HasPrefix(reason, "network_paths")) {
			t.Fatalf("partial_reasons carries %q on a route that emits no such array", reason)
		}
	}
}
