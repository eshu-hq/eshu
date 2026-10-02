// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// worstCaseContextHandler is populatedContextHandler with the artifact rows at
// the read-model cap of 50 in total (25 outgoing, 25 incoming), the shape
// production serves from the Postgres read model.
func worstCaseContextHandler() *Handler {
	h := populatedContextHandler()
	reader := h.Neo4j.(graph.FakeWorkloadGraphReader)
	reader.RunByMatch["RETURN 'outgoing' AS direction"] = budgetArtifactRows("outgoing", 25)
	reader.RunByMatch["RETURN 'incoming' AS direction"] = budgetArtifactRows("incoming", 25)
	// The service route resolves the name through the defining-repository
	// lookup; it answers the same workload the workload route reads by id.
	reader.RunByMatch["collect(DISTINCT dr.id) as defining"] = []map[string]any{{
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
	}}
	h.Neo4j = reader
	return h
}

// getContextEnvelope serves path through the real mux and returns the status,
// the whole response envelope, and est2x (twice the body, the MCP budget
// metric). It does not fail on a non-200 status.
func getContextEnvelope(t *testing.T, h *Handler, path, param, value string) (int, map[string]any, int) {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue(param, value)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var envelope map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}
	return w.Code, envelope, 2 * len(w.Body.Bytes())
}

func omissionSections(t *testing.T, envelope map[string]any) map[string]float64 {
	t.Helper()
	truth, _ := envelope["truth"].(map[string]any)
	raw, _ := truth["omissions"].([]any)
	out := map[string]float64{}
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		section, _ := entry["section"].(string)
		total, _ := entry["total"].(float64)
		if detail, _ := entry["detail"].(string); detail != "handles" {
			t.Fatalf("omission %v detail = %q, want handles", entry, detail)
		}
		out[section] = total
	}
	return out
}

// TestContextRoutesHandlesModeFitsWorstCaseBudget drives both context routes
// over the worst-case fixture. The default HTTP shape (full) is over the MCP
// budget on it, which is what makes the fixture meaningful; handles mode, the
// MCP default, fits with room to spare, keeps every count, projects rows to
// identity keys, and reports each reduced family as a truth omission (#7129).
func TestContextRoutesHandlesModeFitsWorstCaseBudget(t *testing.T) {
	t.Parallel()

	routes := []struct{ name, path, param, value string }{
		{"workload", "/api/v0/workloads/workload-1/context", "workload_id", "workload-1"},
		{"service", "/api/v0/services/svc/context", "service_name", "svc"},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			status, _, fullBytes := getContextEnvelope(t, worstCaseContextHandler(), route.path, route.param, route.value)
			if status != http.StatusOK {
				t.Fatalf("full status = %d, want 200", status)
			}
			if fullBytes <= mcpResponseByteBudget {
				t.Fatalf("full est2x = %d, want > %d so the fixture can tell the modes apart", fullBytes, mcpResponseByteBudget)
			}

			status, envelope, handlesBytes := getContextEnvelope(t, worstCaseContextHandler(), route.path+"?evidence_detail=handles", route.param, route.value)
			if status != http.StatusOK {
				t.Fatalf("handles status = %d, want 200", status)
			}
			t.Logf("%s worst case est2x: full %d, handles %d (budget %d)", route.name, fullBytes, handlesBytes, mcpResponseByteBudget)
			if handlesBytes > mcpResponseByteBudget*3/4 {
				t.Fatalf("handles est2x = %d, want <= %d (75%% of the budget)", handlesBytes, mcpResponseByteBudget*3/4)
			}
			data, _ := envelope["data"].(map[string]any)
			if got := querycontract.StringVal(data, "evidence_detail"); got != "handles" {
				t.Fatalf("evidence_detail = %q, want handles", got)
			}
			evidence := querycontract.MapValue(data, "deployment_evidence")
			artifacts := querycontract.MapSliceValue(evidence, "artifacts")
			if len(artifacts) != 50 || len(artifacts[0]) != 3 {
				t.Fatalf("artifacts = %d rows of %d keys, want 50 handle rows of 3", len(artifacts), len(artifacts[0]))
			}
			if got := querycontract.IntVal(evidence, "artifact_count"); got != 50 {
				t.Fatalf("artifact_count = %d, want 50", got)
			}
			endpoints := querycontract.MapSliceValue(querycontract.MapValue(data, "api_surface"), "endpoints")
			if len(endpoints) != 50 || len(endpoints[0]) != 3 {
				t.Fatalf("endpoints = %d rows of %d keys, want 50 handle rows of 3", len(endpoints), len(endpoints[0]))
			}
			if got := querycontract.IntVal(querycontract.MapValue(data, "api_surface"), "endpoint_count"); got != budgetEndpointTotal {
				t.Fatalf("endpoint_count = %d, want %d", got, budgetEndpointTotal)
			}
			sections := omissionSections(t, envelope)
			if sections["deployment_evidence.artifacts"] != 50 || sections["api_surface.endpoints"] != budgetEndpointTotal {
				t.Fatalf("truth.omissions = %v, want artifacts 50 and endpoints %d", sections, budgetEndpointTotal)
			}
		})
	}
}

// TestContextRoutesRejectUnknownEvidenceDetail proves an unknown value is a
// 400 invalid_argument before any read runs, and that full leaves the response
// without omissions.
func TestContextRoutesRejectUnknownEvidenceDetail(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/workloads/workload-1/context?evidence_detail=compact",
		"/api/v0/services/svc/context?evidence_detail=compact",
	} {
		param, value := "workload_id", "workload-1"
		if path[len("/api/v0/")] == 's' {
			param, value = "service_name", "svc"
		}
		status, envelope, _ := getContextEnvelope(t, worstCaseContextHandler(), path, param, value)
		if status != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", path, status)
		}
		errEnvelope, _ := envelope["error"].(map[string]any)
		if got, _ := errEnvelope["code"].(string); got != string(querycontract.ErrorCodeInvalidArgument) {
			t.Fatalf("%s error code = %q, want invalid_argument", path, got)
		}
	}

	status, envelope, _ := getContextEnvelope(t, worstCaseContextHandler(), "/api/v0/workloads/workload-1/context?evidence_detail=full", "workload_id", "workload-1")
	if status != http.StatusOK {
		t.Fatalf("full status = %d, want 200", status)
	}
	if got := omissionSections(t, envelope); len(got) != 0 {
		t.Fatalf("full truth.omissions = %v, want none", got)
	}
}
