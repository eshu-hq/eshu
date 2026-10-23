// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// callMetricsForTest installs a manual-reader meter provider and rebinds both
// the budget instruments and the dispatch call counter to it.
func callMetricsForTest(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	resetDispatchBudgetMetricsForTest()
	resetDispatchCallMetricsForTest()
	t.Cleanup(func() {
		resetDispatchBudgetMetricsForTest()
		resetDispatchCallMetricsForTest()
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return reader
}

// collectToolCalls returns eshu_dp_mcp_tool_calls_total keyed by "tool/outcome".
func collectToolCalls(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_mcp_tool_calls_total" {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s type = %T, want a counter", m.Name, m.Data)
			}
			for _, dp := range data.DataPoints {
				tool, _ := dp.Attributes.Value("tool")
				outcome, _ := dp.Attributes.Value("outcome")
				got[tool.AsString()+"/"+outcome.AsString()] += dp.Value
			}
		}
	}
	return got
}

func requireToolCalls(t *testing.T, reader *sdkmetric.ManualReader, want map[string]int64) {
	t.Helper()
	got := collectToolCalls(t, reader)
	if len(got) != len(want) {
		t.Fatalf("tool call series = %v, want %v", got, want)
	}
	for key, n := range want {
		if got[key] != n {
			t.Fatalf("tool call series = %v, want %v", got, want)
		}
	}
}

func dispatchForCall(
	ctx context.Context, handler http.Handler, tool string, args map[string]any, opts dispatchOptions,
) (*dispatchResult, error) {
	return dispatchToolWithOptions(ctx, handler, tool, args, "", discardDispatchLogger(), opts)
}

func TestDispatchCallCounterLabelsEveryDispatchEnding(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	rowsOK := bigRowsHandler(t, 2, 16)
	fail := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	envelopeFail := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query.WriteErrorEnvelope(w, r, http.StatusNotFound, &query.ErrorEnvelope{Code: query.ErrorCodeNotFound, Message: "missing"})
	})
	hang := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	partial := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"deadline_truncated":true,"rows":[]},"truth":{"level":"partial"},"error":null}`))
		<-r.Context().Done()
	})
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("handler panic") })
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	symbol := map[string]any{"symbol": "Handle", "limit": 5}
	budgeted := dispatchOptions{responseByteBudget: defaultToolResponseByteBudget}

	cases := []struct {
		name    string
		handler http.Handler
		ctx     context.Context
		tool    string
		args    map[string]any
		opts    dispatchOptions
		want    string
		panics  bool
	}{
		{name: "ok", handler: rowsOK, tool: "find_symbol", args: symbol, opts: budgeted, want: "find_symbol/ok"},
		{name: "handler http failure", handler: fail, tool: "find_symbol", args: symbol, opts: budgeted, want: "find_symbol/error"},
		{name: "envelope failure", handler: envelopeFail, tool: "find_symbol", args: symbol, opts: budgeted, want: "find_symbol/error"},
		{name: "unknown tool is bucketed", handler: rowsOK, tool: "no_such_tool_a1b2c3", args: symbol, opts: budgeted, want: "unknown/unknown_tool"},
		{name: "registered tool, bad arguments", handler: rowsOK, tool: "get_workload_context", args: map[string]any{}, opts: budgeted, want: "get_workload_context/invalid_request"},
		{name: "over budget", handler: bigRowsHandler(t, 200, 256), tool: "find_symbol", args: symbol, opts: dispatchOptions{responseByteBudget: 4 * 1024}, want: "find_symbol/over_budget"},
		{name: "canceled context", handler: rowsOK, ctx: canceled, tool: "find_symbol", args: symbol, opts: budgeted, want: "find_symbol/canceled"},
		{name: "deadline", handler: hang, tool: "find_symbol", args: symbol, opts: dispatchOptions{timeout: 20 * time.Millisecond}, want: "find_symbol/timeout"},
		{name: "deadline with bounded partial", handler: partial, tool: partialCapableToolName, args: map[string]any{"topic": "x"}, opts: dispatchOptions{timeout: 20 * time.Millisecond, responseByteBudget: defaultToolResponseByteBudget}, want: partialCapableToolName + "/partial_timeout"},
		{name: "handler panic", handler: boom, tool: "find_symbol", args: symbol, opts: budgeted, want: "find_symbol/panic", panics: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := callMetricsForTest(t)
			ctx := tc.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			func() {
				defer func() {
					if r := recover(); (r != nil) != tc.panics {
						t.Fatalf("recover() = %v, want panic = %v", r, tc.panics)
					}
				}()
				_, _ = dispatchForCall(ctx, tc.handler, tc.tool, tc.args, tc.opts)
			}()
			requireToolCalls(t, reader, map[string]int64{tc.want: 1})
		})
	}
}

// TestDispatchCallCounterNeverLabelsRawClientNames proves many distinct
// unknown names collapse to one series, so a client cannot grow the label set.
func TestDispatchCallCounterNeverLabelsRawClientNames(t *testing.T) {
	reader := callMetricsForTest(t)
	for _, name := range []string{"x1", "x2", "", "find_symbol ", "FIND_SYMBOL"} {
		_, _ = dispatchForCall(context.Background(), bigRowsHandler(t, 1, 1), name, nil, dispatchOptions{})
	}
	requireToolCalls(t, reader, map[string]int64{"unknown/unknown_tool": 5})
}
