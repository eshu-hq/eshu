// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// budgetMetricsForTest installs a manual-reader meter provider as the global
// provider and rebinds the lazily registered budget instruments to it.
func budgetMetricsForTest(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	resetDispatchBudgetMetricsForTest()
	t.Cleanup(func() {
		resetDispatchBudgetMetricsForTest()
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return reader
}

// collectBudgetMetrics returns the over-budget counter total and the response
// bytes histogram (count, sum) for one tool label.
func collectBudgetMetrics(t *testing.T, reader *sdkmetric.ManualReader, tool string) (overBudget int64, count uint64, sum int64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				if m.Name != "eshu_dp_mcp_response_over_budget_total" {
					continue
				}
				for _, dp := range data.DataPoints {
					if v, ok := dp.Attributes.Value("tool"); ok && v.AsString() == tool {
						overBudget += dp.Value
					}
				}
			case metricdata.Histogram[int64]:
				if m.Name != "eshu_dp_mcp_response_bytes" {
					continue
				}
				for _, dp := range data.DataPoints {
					if v, ok := dp.Attributes.Value("tool"); ok && v.AsString() == tool {
						count += dp.Count
						sum += dp.Sum
					}
				}
			}
		}
	}
	return overBudget, count, sum
}

// TestApplyResponseBudgetRecordsSizeAndOverBudgetPerTool proves the operator
// signals for #7129: every budgeted response records its wire size under the
// tool label, and only a response the guard replaces increments the
// over-budget counter. verify-telemetry-coverage proves registration; this
// proves emission through the production dispatch path.
func TestApplyResponseBudgetRecordsSizeAndOverBudgetPerTool(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	reader := budgetMetricsForTest(t)

	small := bigRowsHandler(t, 2, 16)
	res, err := dispatchWithBudget(t, small, defaultToolResponseByteBudget)
	if err != nil || res.IsError {
		t.Fatalf("small dispatch = (%+v, %v), want an under-budget success", res, err)
	}
	over, count, sum := collectBudgetMetrics(t, reader, "find_code")
	if over != 0 {
		t.Fatalf("over-budget counter = %d after an under-budget response, want 0", over)
	}
	if count != 1 || sum <= 0 {
		t.Fatalf("response bytes histogram = (count %d, sum %d), want one positive sample", count, sum)
	}

	res, err = dispatchWithBudget(t, bigRowsHandler(t, 200, 256), 4*1024)
	if err != nil || !res.IsError {
		t.Fatalf("big dispatch = (%+v, %v), want the over-budget error envelope", res, err)
	}
	over, count, _ = collectBudgetMetrics(t, reader, "find_code")
	if over != 1 {
		t.Fatalf("over-budget counter = %d after one over-budget response, want 1", over)
	}
	if count != 2 {
		t.Fatalf("response bytes histogram count = %d, want 2 (both responses sized)", count)
	}
}

// TestApplyResponseBudgetDisabledRecordsNothing keeps the disabled-guard
// contract (budget <= 0 returns the result unchanged) free of side effects.
func TestApplyResponseBudgetDisabledRecordsNothing(t *testing.T) {
	reader := budgetMetricsForTest(t)

	if _, err := dispatchWithBudget(t, bigRowsHandler(t, 2, 16), 0); err != nil {
		t.Fatalf("dispatchWithBudget(budget=0) error = %v, want nil", err)
	}
	if over, count, _ := collectBudgetMetrics(t, reader, "find_code"); over != 0 || count != 0 {
		t.Fatalf("disabled guard recorded (over %d, count %d), want none", over, count)
	}
}
