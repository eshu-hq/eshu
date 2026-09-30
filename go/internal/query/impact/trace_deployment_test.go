// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestTraceDeploymentChainReturnsConflictForDuplicateWorkloadName(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return nil, nil
		}
		if strings.Contains(cypher, "w.name = $service_name") {
			return []map[string]any{
				{"id": "workload:orders-a", "name": "orders"},
				{"id": "workload:orders-b", "name": "orders"},
			}, nil
		}
		return nil, nil
	}}
	handler := &Handler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"orders"}`))
	recorder := httptest.NewRecorder()

	handler.TraceDeploymentChain(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestTraceDeploymentChainClampsAbsurdMaxDepthInsteadOfRejecting(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		maxDepth       int
		wantTraceLimit int
	}{
		{
			name:           "negative max_depth 200s and falls through to the operator-safe default limit",
			maxDepth:       -1,
			wantTraceLimit: querycontract.DefaultIndirectEvidenceSearchLimit,
		},
		{
			name:           "overflow-inducing max_depth 200s and resolves to the saturated package-cap limit",
			maxDepth:       922337203685477581,
			wantTraceLimit: querycontract.MaxIndirectEvidenceSearchLimit,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workload := map[string]any{
				"id":        "workload:orders-api",
				"instances": []any{},
				"kind":      "service",
				"name":      "orders-api",
				"repo_id":   "repo-orders",
				"repo_name": "orders-api",
			}
			// Empty in-memory content: the SQL decoding layer stays covered by the
			// root content_reader tests; impact/ tests cannot import package
			// query. See #6060.
			var sawProvisioningQuery bool
			var gotLimit any
			handler := &Handler{
				Neo4j: graph.FakeWorkloadGraphReader{
					RunSingleByMatch: map[string]map[string]any{
						"w.name = $service_name": workload,
						"w.id = $workload_id":    workload,
					},
					RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
						if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM") {
							sawProvisioningQuery = true
							gotLimit = params["limit"]
							return nil, nil
						}
						if strings.Contains(cypher, "w.name = $service_name") {
							return []map[string]any{workload}, nil
						}
						if strings.Contains(cypher, "DEFINES]-(r:Repository)") {
							return []map[string]any{{"repo_id": "repo-orders", "repo_name": "orders-api"}}, nil
						}
						return nil, nil
					},
				},
				Content: &content.FakePortContentStore{},
			}

			body := fmt.Sprintf(`{"service_name":"orders-api","max_depth":%d}`, tc.maxDepth)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(body))
			recorder := httptest.NewRecorder()

			handler.TraceDeploymentChain(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if !sawProvisioningQuery {
				t.Fatal("provisioning candidates query never ran; cannot observe the clamped max_depth")
			}
			// #5720 round-2 P1-1: queryProvisioningRepositoryCandidates now
			// probes one row past the disclosed limit to detect truncation,
			// so the wire-visible bound is wantTraceLimit+1.
			if wantWireLimit := tc.wantTraceLimit + 1; gotLimit != wantWireLimit {
				t.Fatalf("provisioning candidates limit = %#v, want %d (max_depth=%d must clamp, not reject)", gotLimit, wantWireLimit, tc.maxDepth)
			}
		})
	}
}

func TestNormalizeTraceDeploymentChainMaxDepth(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		maxDepth int
		want     int
	}{
		{name: "negative clamps to zero", maxDepth: -1, want: 0},
		{name: "math.MinInt clamps to zero", maxDepth: math.MinInt, want: 0},
		{name: "zero passes through unchanged", maxDepth: 0, want: 0},
		{name: "at the limit passes through unchanged", maxDepth: 1000, want: traceDeploymentChainMaxDepthLimit},
		{name: "just above the limit clamps down", maxDepth: 1001, want: traceDeploymentChainMaxDepthLimit},
		{name: "overflow-scale value clamps down", maxDepth: 922337203685477581, want: traceDeploymentChainMaxDepthLimit},
		{name: "math.MaxInt clamps down", maxDepth: math.MaxInt, want: traceDeploymentChainMaxDepthLimit},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeTraceDeploymentChainMaxDepth(tc.maxDepth); got != tc.want {
				t.Fatalf("normalizeTraceDeploymentChainMaxDepth(%d) = %d, want %d", tc.maxDepth, got, tc.want)
			}
		})
	}
}

// minimalTraceHandler serves one resolvable workload with no evidence, enough
// to drive TraceDeploymentChain to a 200.
func minimalTraceHandler() *Handler {
	workload := map[string]any{
		"id": "workload:orders-api", "instances": []any{}, "kind": "service",
		"name": "orders-api", "repo_id": "repo-orders", "repo_name": "orders-api",
	}
	return &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "w.name = $service_name") {
					return []map[string]any{workload}, nil
				}
				if strings.Contains(cypher, "DEFINES]-(r:Repository)") {
					return []map[string]any{{"repo_id": "repo-orders", "repo_name": "orders-api"}}, nil
				}
				return nil, nil
			},
		},
		Content: &content.FakePortContentStore{},
	}
}

func serveTrace(t *testing.T, handler *Handler, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(body))
	req.Header.Set("Accept", "application/eshu.envelope+json")
	recorder := httptest.NewRecorder()
	handler.TraceDeploymentChain(recorder, req)
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return recorder.Code, envelope
}

// TestTraceDeploymentChainRejectsUnknownSectionSelection proves an unknown
// sections or evidence_detail value is a 400 invalid_argument naming the
// allowed values (#7174), never a silently ignored selection.
func TestTraceDeploymentChainRejectsUnknownSectionSelection(t *testing.T) {
	t.Parallel()

	for body, wantInMessage := range map[string]string{
		`{"service_name":"orders-api","sections":["instances","bogus"]}`: "delivery_paths",
		`{"service_name":"orders-api","evidence_detail":"summary"}`:      "handles",
	} {
		code, envelope := serveTrace(t, minimalTraceHandler(), body)
		errEnv, _ := envelope["error"].(map[string]any)
		message, _ := errEnv["message"].(string)
		if code != http.StatusBadRequest || errEnv["code"] != "invalid_argument" || !strings.Contains(message, wantInMessage) {
			t.Fatalf("body %s: status %d error %v, want 400 invalid_argument naming %q", body, code, errEnv, wantInMessage)
		}
	}
}

// TestTraceDeploymentChainHTTPDefaultStaysFull proves an HTTP body without the
// #7174 fields keeps the full response: evidence_detail "full", every
// section_detail entry "full", and no truth.omissions key. The handles case
// proves the omission reaches the truth envelope.
func TestTraceDeploymentChainHTTPDefaultStaysFull(t *testing.T) {
	t.Parallel()

	code, envelope := serveTrace(t, minimalTraceHandler(), `{"service_name":"orders-api"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, envelope)
	}
	data, _ := envelope["data"].(map[string]any)
	truth, _ := envelope["truth"].(map[string]any)
	if data["evidence_detail"] != "full" {
		t.Fatalf("evidence_detail = %v, want full", data["evidence_detail"])
	}
	sectionDetail, _ := data["section_detail"].(map[string]any)
	if len(sectionDetail) == 0 {
		t.Fatal("section_detail missing from the full response")
	}
	for family, entry := range sectionDetail {
		if detail, _ := entry.(map[string]any); detail["detail"] != "full" {
			t.Fatalf("section_detail[%s] = %v, want full", family, entry)
		}
	}
	if _, ok := truth["omissions"]; ok {
		t.Fatalf("truth.omissions present on the full HTTP default: %v", truth["omissions"])
	}
	if _, ok := data["delivery_paths"]; !ok {
		t.Fatal("full HTTP default dropped delivery_paths")
	}

	code, envelope = serveTrace(t, minimalTraceHandler(), `{"service_name":"orders-api","evidence_detail":"handles"}`)
	if code != http.StatusOK {
		t.Fatalf("handles status = %d, body = %v", code, envelope)
	}
	data, _ = envelope["data"].(map[string]any)
	truth, _ = envelope["truth"].(map[string]any)
	if _, ok := data["delivery_paths"]; ok {
		t.Fatal("handles request emitted delivery_paths")
	}
	omissions, _ := truth["omissions"].([]any)
	found := false
	for _, raw := range omissions {
		if omission, _ := raw.(map[string]any); omission["section"] == "delivery_paths" && omission["detail"] == "omitted" {
			found = true
		}
	}
	if !found {
		t.Fatalf("truth.omissions = %v, want delivery_paths omitted", truth["omissions"])
	}
}
