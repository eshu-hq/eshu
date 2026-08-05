// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTraceDeploymentChainReturnsConflictForDuplicateWorkloadName(t *testing.T) {
	t.Parallel()

	call := 0
	reader := fakeGraphReader{runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return nil, nil
		}
		if strings.Contains(cypher, "w.name = $service_name") {
			call++
			return map[string]any{"id": "workload:orders-" + string(rune('a'+call-1))}, nil
		}
		return nil, nil
	}}
	handler := &ImpactHandler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"orders"}`))
	recorder := httptest.NewRecorder()

	handler.traceDeploymentChain(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestResolveTraceWorkloadSelectorRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		switch {
		case strings.Contains(cypher, "w.id = $service_name"):
			return nil, nil
		case strings.Contains(cypher, "w.name = $service_name") && strings.Contains(cypher, "SKIP 1"):
			if !strings.Contains(cypher, "ORDER BY w.id") {
				t.Fatalf("name selector query = %q, want deterministic ambiguity probe", cypher)
			}
			return map[string]any{"id": "workload:orders-b"}, nil
		case strings.Contains(cypher, "w.name = $service_name"):
			return map[string]any{"id": "workload:orders-a"}, nil
		default:
			t.Fatalf("unexpected query: %s", cypher)
			return nil, nil
		}
	}}

	_, err := resolveTraceWorkloadSelector(t.Context(), reader, "orders")
	if !errors.Is(err, errAmbiguousTraceWorkloadSelector) {
		t.Fatalf("resolveTraceWorkloadSelector() error = %v, want ambiguity", err)
	}
}

// TestTraceDeploymentChainClampsAbsurdMaxDepthInsteadOfRejecting is the
// #5720 P2-3 handler-level half of the overflow fix, retargeted in round 2
// (P1-2): the first draft of this boundary rejected an absurd max_depth with
// 400, but that broke the wire contract every sibling max_depth-bearing
// route keeps (impact_resource_investigation.go,
// impact_change_surface_investigation.go, impact_change_surface_legacy.go
// all normalize rather than reject) and silently changed observable
// behavior for existing callers, including the MCP dispatch route which
// forwards an explicit max_depth straight through. A negative max_depth, and
// a max_depth large enough that boundedTraceEnrichmentLimit's `maxDepth *
// 10` would overflow int64 if it ever reached that function unclamped, must
// both 200 rather than reject. This drives the real request through the real
// handler and observes the wire-visible limit param the
// provisioning-candidates Cypher query (deployment_trace_support_helpers.go)
// actually receives.
//
// #5720 round-4 P2: this HTTP-level assertion alone cannot distinguish
// whether normalizeTraceDeploymentChainMaxDepth (impact_trace_deployment.go)
// actually ran, because boundedTraceEnrichmentLimit maps every int input --
// including both cases below -- into (0, maxIndirectEvidenceSearchLimit] on
// its own; deleting the handler clamp yields the identical wantTraceLimit for
// both. What this test genuinely proves is the response-code half of the
// contract: out-of-range max_depth 200s instead of rejecting. See
// TestNormalizeTraceDeploymentChainMaxDepth for the clamp's own boundary
// behavior, proven directly against the extracted pure function.
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
			wantTraceLimit: defaultIndirectEvidenceSearchLimit,
		},
		{
			name:           "overflow-inducing max_depth 200s and resolves to the saturated package-cap limit",
			maxDepth:       922337203685477581,
			wantTraceLimit: maxIndirectEvidenceSearchLimit,
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
			db := openContentReaderTestDB(t, emptyServiceQueryContentResults())

			var sawProvisioningQuery bool
			var gotLimit any
			handler := &ImpactHandler{
				Neo4j: fakeWorkloadGraphReader{
					runSingleByMatch: map[string]map[string]any{
						"w.name = $service_name": workload,
						"w.id = $workload_id":    workload,
					},
					run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
						if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM") {
							sawProvisioningQuery = true
							gotLimit = params["limit"]
							return nil, nil
						}
						if strings.Contains(cypher, "DEFINES]-(r:Repository)") {
							return []map[string]any{{"repo_id": "repo-orders", "repo_name": "orders-api"}}, nil
						}
						return nil, nil
					},
				},
				Content: NewContentReader(db),
			}

			body := fmt.Sprintf(`{"service_name":"orders-api","max_depth":%d}`, tc.maxDepth)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(body))
			recorder := httptest.NewRecorder()

			handler.traceDeploymentChain(recorder, req)

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

func TestResolveTraceWorkloadSelectorPreservesExactIDLookup(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		if !strings.Contains(cypher, "w.id = $service_name") {
			t.Fatalf("first query = %q, want exact id lookup", cypher)
		}
		return map[string]any{"id": "workload:orders"}, nil
	}}

	got, err := resolveTraceWorkloadSelector(t.Context(), reader, "workload:orders")
	if err != nil || got != "workload:orders" {
		t.Fatalf("resolveTraceWorkloadSelector() = %q, %v, want exact workload id", got, err)
	}
}

// TestNormalizeTraceDeploymentChainMaxDepth is the #5720 round-4 P2 direct
// unit proof for normalizeTraceDeploymentChainMaxDepth
// (impact_trace_deployment.go), extracted from the inline handler clamp so
// its boundary behavior has coverage independent of
// boundedTraceEnrichmentLimit's own saturation (see the reworded doc comment
// on TestTraceDeploymentChainClampsAbsurdMaxDepthInsteadOfRejecting above for
// why the HTTP-level test alone cannot prove this). Mutation-proof: neutering
// the clamp body to `return maxDepth` fails 5 of the 7 cases below;
// maxDepth=0 and maxDepth=1000 are identity cases (the clamp is a no-op at
// those inputs even before mutation) and survive the mutant by construction.
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
