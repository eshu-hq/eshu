// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestResolveTraceWorkloadSelectorRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
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

	_, err := ResolveTraceWorkloadSelector(t.Context(), reader, "orders")
	if !errors.Is(err, errAmbiguousTraceWorkloadSelector) {
		t.Fatalf("ResolveTraceWorkloadSelector() error = %v, want ambiguity", err)
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

func TestResolveTraceWorkloadSelectorPreservesExactIDLookup(t *testing.T) {
	t.Parallel()

	reader := querytestutil.FakeGraphReader{RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		if !strings.Contains(cypher, "w.id = $service_name") {
			t.Fatalf("first query = %q, want exact id lookup", cypher)
		}
		return map[string]any{"id": "workload:orders"}, nil
	}}

	got, err := ResolveTraceWorkloadSelector(t.Context(), reader, "workload:orders")
	if err != nil || got != "workload:orders" {
		t.Fatalf("ResolveTraceWorkloadSelector() = %q, %v, want exact workload id", got, err)
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
