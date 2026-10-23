// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func benchDispatchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func benchCallMeter(b *testing.B) {
	b.Helper()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	resetDispatchBudgetMetricsForTest()
	resetDispatchCallMetricsForTest()
	b.Cleanup(func() {
		resetDispatchBudgetMetricsForTest()
		resetDispatchCallMetricsForTest()
		otel.SetMeterProvider(previous)
	})
}

// BenchmarkDispatchWithoutCallCounter is the dispatch body alone, the baseline
// the counter's cost is read against.
func BenchmarkDispatchWithoutCallCounter(b *testing.B) {
	benchCallMeter(b)
	handler, logger := benchDispatchHandler(), discardDispatchLogger()
	opts := dispatchOptions{timeout: defaultToolDispatchTimeout, responseByteBudget: defaultToolResponseByteBudget}
	args := map[string]any{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var call dispatchCall
		_, _ = runDispatch(context.Background(), handler, "list_indexed_repositories", args, "", logger, opts, &call)
	}
}

// BenchmarkDispatchWithCallCounter is the production entry, counter included.
func BenchmarkDispatchWithCallCounter(b *testing.B) {
	benchCallMeter(b)
	handler, logger := benchDispatchHandler(), discardDispatchLogger()
	opts := dispatchOptions{timeout: defaultToolDispatchTimeout, responseByteBudget: defaultToolResponseByteBudget}
	args := map[string]any{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = dispatchToolWithOptions(context.Background(), handler, "list_indexed_repositories", args, "", logger, opts)
	}
}

// BenchmarkCallCounterWriteOnly isolates the counter write with prebuilt
// options (shipped) against per-call attribute construction (rejected).
func BenchmarkCallCounterWriteOnly(b *testing.B) {
	benchCallMeter(b)
	ctx := context.Background()
	b.Run("prebuilt_options", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			call := startDispatchCall("list_indexed_repositories")
			call.finish(ctx, &dispatchResult{}, nil, true)
		}
	})
	b.Run("per_call_attributes", func(b *testing.B) {
		inst := dispatchCallMetrics()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			inst.calls.Add(ctx, 1, metric.WithAttributes(
				attribute.String("tool", "list_indexed_repositories"), attribute.String("outcome", "ok")))
		}
	})
}
