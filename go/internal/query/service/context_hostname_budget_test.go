// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// mcpResponseByteBudget mirrors the MCP dispatch budget
// (go/internal/mcp/dispatch_budget.go defaultToolResponseByteBudget): 256 KiB
// of serialized response, counted twice because the wire carries the payload as
// structuredContent and again as escaped resource text.
const mcpResponseByteBudget = 256 * 1024

// hostnameEntrypointBudgetFixture returns a workload context shaped like the
// measured outlier from #7169: one service with 671 hostnames and 671
// entrypoints, each list also copied into deployment_overview by the old
// shape.
func hostnameEntrypointBudgetFixture() map[string]any {
	const rowCount = 671
	hostnames := make([]map[string]any, 0, rowCount)
	entrypoints := make([]map[string]any, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		host := fmt.Sprintf("svc-%03d.tenant.qa.example.test", i)
		hostnames = append(hostnames, map[string]any{
			"hostname":      host,
			"environment":   "qa",
			"relative_path": "deploy/charts/service/values-qa.yaml",
			"reason":        "hostname literal in deployment values",
		})
		entrypoints = append(entrypoints, map[string]any{
			"type":          "hostname",
			"target":        host,
			"environment":   "qa",
			"visibility":    "public",
			"relative_path": "deploy/charts/service/values-qa.yaml",
			"reason":        "hostname literal in deployment values",
		})
	}
	return map[string]any{
		"id":          "workload:svc",
		"name":        "svc",
		"kind":        "service",
		"repo_id":     "repo-svc",
		"repo_name":   "svc",
		"hostnames":   hostnames,
		"entrypoints": entrypoints,
	}
}

func estimatedWireBytes(t *testing.T, payload map[string]any) int {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return 2 * len(encoded)
}

// TestHostnameEntrypointOutlierStaysUnderMCPBudget proves the 671-hostname /
// 671-entrypoint outlier fits the MCP response budget on the three routes
// that carry it: get_workload_context, get_service_context (both the shared
// WorkloadContext handler tail) and trace_deployment_chain (#7169).
func TestHostnameEntrypointOutlierStaysUnderMCPBudget(t *testing.T) {
	t.Parallel()

	// Guard against a vacuous fixture: the raw duplicated shape must be over
	// budget, or the assertions below prove nothing.
	raw := hostnameEntrypointBudgetFixture()
	raw["deployment_overview"] = map[string]any{
		"hostnames":   raw["hostnames"],
		"entrypoints": raw["entrypoints"],
	}
	if got := estimatedWireBytes(t, raw); got <= mcpResponseByteBudget {
		t.Fatalf("fixture est2x = %d, want over the %d budget so the test can discriminate", got, mcpResponseByteBudget)
	}

	t.Run("workload and service context", func(t *testing.T) {
		t.Parallel()
		ctx := hostnameEntrypointBudgetFixture()
		ctx["deployment_overview"] = buildServiceDeploymentOverviewWithContext(newServiceStoryBuildContext(ctx))
		ctx["result_limits"] = querycontract.WorkloadContextResultLimits(ctx, "workload:svc", "context")
		ctx["partial_reasons"] = querycontract.ContextPartialReasons(ctx)
		if got := estimatedWireBytes(t, ctx); got > mcpResponseByteBudget {
			t.Fatalf("context est2x = %d bytes, want <= %d", got, mcpResponseByteBudget)
		}
		if n := len(querycontract.MapSliceValue(ctx, "hostnames")); n != querycontract.ContextStoryItemLimit {
			t.Fatalf("hostnames len = %d, want %d", n, querycontract.ContextStoryItemLimit)
		}
	})

	t.Run("trace deployment chain", func(t *testing.T) {
		t.Parallel()
		ctx := hostnameEntrypointBudgetFixture()
		overview := buildServiceDeploymentOverviewWithContext(newServiceStoryBuildContext(ctx))
		response := deployment.BuildDeploymentTraceResponse("svc", ctx, overview)
		if got := estimatedWireBytes(t, response); got > mcpResponseByteBudget {
			t.Fatalf("trace est2x = %d bytes, want <= %d", got, mcpResponseByteBudget)
		}
	})
}
