// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"testing"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// dispatchShaped dispatches find_symbol (a tool that does not page) under
// shape with budget.
func dispatchShaped(t *testing.T, mode resultshape.Mode, rows, rowBytes, budget int) *dispatchResult {
	t.Helper()
	ctx := resultshape.WithOverride(context.Background(), mode)
	result, err := dispatchToolWithOptions(ctx, bigRowsHandler(t, rows, rowBytes), "find_symbol",
		map[string]any{"symbol": "Handle", "limit": 5}, "", discardDispatchLogger(), dispatchOptions{responseByteBudget: budget})
	if err != nil {
		t.Fatalf("dispatch error = %v, want nil", err)
	}
	return result
}

// TestStructuredSingleCopyBetween130And256KiBPassesUntouched is the inverse of
// TestDispatchToolResponseDoubledWireUsesFullResource: with one copy on the
// wire, a 130 to 256 KiB envelope fits, so neither the resource fallback nor
// a page applies and structuredContent stays.
func TestStructuredSingleCopyBetween130And256KiBPassesUntouched(t *testing.T) {
	t.Parallel()
	const budget = defaultToolResponseByteBudget
	for _, rows := range []int{300, 440} { // about 156 KiB and about 233 KiB per copy
		single := singleEnvelopeMarshalBytes(t, bigRowsHandler(t, rows, 512))
		if single < 130*1024 || single >= budget {
			t.Fatalf("rows=%d single copy = %d bytes, want within [130 KiB, %d)", rows, single, budget)
		}
		result := dispatchShaped(t, resultshape.Structured, rows, 512, budget)
		if result.IsError || result.ResourceOnly || result.Envelope == nil || result.Envelope.Error != nil {
			t.Fatalf("rows=%d structured single copy was altered: %#v", rows, result)
		}
		rendered := renderToolResult("find_symbol", result)
		requireOneTextAndStructured(t, rendered)
		wire := len(marshalForCompare(t, rendered))
		if wire <= single || wire > budget {
			t.Fatalf("rows=%d wire size = %d bytes, want in (%d, %d]", rows, wire, single, budget)
		}
		if got := estimateResponseBytes(result); got != wire {
			t.Fatalf("rows=%d estimate = %d, serialized result = %d", rows, got, wire)
		}
	}
}

// TestStructuredOverBudgetSingleCopyNeverFallsBackToResource proves a single
// copy over the limit takes the error path, not the dual-only resource
// fallback, and that the budget limit itself did not move.
func TestStructuredOverBudgetSingleCopyNeverFallsBackToResource(t *testing.T) {
	t.Parallel()
	if defaultToolResponseByteBudget != 262144 {
		t.Fatalf("defaultToolResponseByteBudget = %d, want 262144", defaultToolResponseByteBudget)
	}
	result := dispatchShaped(t, resultshape.Structured, 600, 512, defaultToolResponseByteBudget)
	if !result.IsError || result.ResourceOnly || result.Envelope.Error == nil || result.Envelope.Error.Code != errorCodeResponseOverBudget {
		t.Fatalf("over-budget structured result = %#v, want the over-budget error envelope", result)
	}
}

// TestStructuredPagesOverBudgetResultsForPagedTools proves find_code and
// search_entity_content answer an over-budget single copy with a page that
// fits by construction in the structured shape, with ResourceOnly never set.
func TestStructuredPagesOverBudgetResultsForPagedTools(t *testing.T) {
	t.Parallel()
	for _, call := range budgetPageCalls {
		t.Run(call.tool, func(t *testing.T) {
			t.Parallel()
			ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
			handler := pagedBudgetMux(pagedBudgetStore{newSourceCacheStore(newBudgetPageEntities(260))})
			result, err := dispatchToolWithOptions(ctx, handler, call.tool, call.args(0), "",
				discardDispatchLogger(), dispatchOptions{responseByteBudget: defaultToolResponseByteBudget})
			if err != nil || result.IsError || result.ResourceOnly {
				t.Fatalf("structured page = (%+v, %v), want a success page without ResourceOnly", result, err)
			}
			data := result.Envelope.Data.(map[string]any)
			if data["truncated"] != true || data["budget_page"] == nil {
				t.Fatalf("data = %#v, want a truncated budget page", data)
			}
			rendered := renderToolResult(call.tool, result)
			requireOneTextAndStructured(t, rendered)
			if wire := len(marshalForCompare(t, rendered)); wire > defaultToolResponseByteBudget {
				t.Fatalf("page wire size = %d, want <= %d", wire, defaultToolResponseByteBudget)
			}
			if got, want := estimateResponseBytes(result), len(marshalForCompare(t, rendered)); got != want {
				t.Fatalf("page estimate = %d, serialized = %d", got, want)
			}
		})
	}
}

// shapeSamples sums the budget instruments by metric name and shape label for
// one tool.
func shapeSamples(t *testing.T, reader *sdkmetric.ManualReader, tool string) map[string]map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]map[string]int64{}
	add := func(name string, attrs attribute.Set, value int64) {
		if v, ok := attrs.Value("tool"); !ok || v.AsString() != tool {
			return
		}
		shape := "<missing>"
		if v, ok := attrs.Value("shape"); ok {
			shape = v.AsString()
		}
		if out[name] == nil {
			out[name] = map[string]int64{}
		}
		out[name][shape] += value
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					add(m.Name, dp.Attributes, dp.Value)
				}
			case metricdata.Histogram[int64]:
				for _, dp := range data.DataPoints {
					add(m.Name, dp.Attributes, int64(dp.Count))
				}
			}
		}
	}
	return out
}

// TestBudgetInstrumentsCarryShape proves all four budget instruments carry the
// bounded shape attribute, and that the resource fallback records only under
// dual (it is zero by construction under structured).
func TestBudgetInstrumentsCarryShape(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	reader := budgetMetricsForTest(t)

	dispatchShaped(t, resultshape.Dual, 300, 512, defaultToolResponseByteBudget)       // resource fallback
	dispatchShaped(t, resultshape.Structured, 300, 512, defaultToolResponseByteBudget) // fits untouched
	dispatchShaped(t, resultshape.Dual, 200, 256, 4*1024)                              // over budget
	dispatchShaped(t, resultshape.Structured, 200, 256, 4*1024)                        // over budget
	ctx := resultshape.WithOverride(context.Background(), resultshape.Structured)
	handler := pagedBudgetMux(pagedBudgetStore{newSourceCacheStore(newBudgetPageEntities(260))})
	if _, err := dispatchToolWithOptions(ctx, handler, "find_code", budgetPageCalls[1].args(0), "",
		discardDispatchLogger(), dispatchOptions{responseByteBudget: defaultToolResponseByteBudget}); err != nil {
		t.Fatalf("page dispatch: %v", err)
	}

	got := shapeSamples(t, reader, "find_symbol")
	if want := map[string]int64{"dual": 2, "structured": 2}; !equalShapeCounts(got["eshu_dp_mcp_response_bytes"], want) {
		t.Fatalf("response bytes by shape = %v, want %v", got["eshu_dp_mcp_response_bytes"], want)
	}
	if want := map[string]int64{"dual": 1, "structured": 1}; !equalShapeCounts(got["eshu_dp_mcp_response_over_budget_total"], want) {
		t.Fatalf("over budget by shape = %v, want %v", got["eshu_dp_mcp_response_over_budget_total"], want)
	}
	if want := map[string]int64{"dual": 1}; !equalShapeCounts(got["eshu_dp_mcp_response_resource_fallback_total"], want) {
		t.Fatalf("resource fallback by shape = %v, want dual only", got["eshu_dp_mcp_response_resource_fallback_total"])
	}
	paged := shapeSamples(t, reader, "find_code")
	if want := map[string]int64{"structured": 1}; !equalShapeCounts(paged["eshu_dp_mcp_response_budget_page_total"], want) {
		t.Fatalf("budget page by shape = %v, want structured 1", paged["eshu_dp_mcp_response_budget_page_total"])
	}
}

func equalShapeCounts(got, want map[string]int64) bool {
	if len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}
