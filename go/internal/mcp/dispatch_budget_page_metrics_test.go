// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// collectBudgetPages returns the budget-page counter total for one tool.
func collectBudgetPages(t *testing.T, reader *sdkmetric.ManualReader, tool string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_mcp_response_budget_page_total" {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("budget page metric type = %T, want counter", m.Data)
			}
			for _, dp := range data.DataPoints {
				if v, ok := dp.Attributes.Value("tool"); ok && v.AsString() == tool {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// TestApplyResponseBudgetRecordsBudgetPageAndLogsRows proves the operator
// signals for a budget page: the per-tool counter increments, the over-budget
// counter does not, and the log line carries rows_returned, rows_available and
// the emitted bytes an operator needs at 3 AM.
func TestApplyResponseBudgetRecordsBudgetPageAndLogsRows(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	reader := budgetMetricsForTest(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	result, err := dispatchToolWithOptions(
		context.Background(), bigRowsHandler(t, 200, 256), "find_code",
		map[string]any{"query": "Handle", "limit": 200}, "", logger,
		dispatchOptions{responseByteBudget: 16 * 1024},
	)
	if err != nil || result.IsError {
		t.Fatalf("dispatch = (%+v, %v), want a budget page", result, err)
	}
	if got := collectBudgetPages(t, reader, "find_code"); got != 1 {
		t.Fatalf("budget page counter = %d, want 1", got)
	}
	if over, _, _ := collectBudgetMetrics(t, reader, "find_code"); over != 0 {
		t.Fatalf("over-budget counter = %d for a page, want 0", over)
	}
	for _, want := range []string{"mcp tool response budget page", "rows_returned=", "rows_available=200", "emitted_bytes=", "budget_bytes=16384"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log %q missing %q", logs.String(), want)
		}
	}
}
