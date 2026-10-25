// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"net/http"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// collect reads every metric the reader holds, keyed by name.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]metricdata.Metrics{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

func TestListRecordsReadDurationAndExcludedDeclarations(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	graph := &fakeGraph{
		pageRows:     []map[string]any{decl("repository:r_a", ".nvmrc", 1, 0, "node", "18", "partial", intp(18))},
		excludedRows: []map[string]any{{"spec_kind": "range", "excluded_count": int64(3)}, {"spec_kind": "alias", "excluded_count": int64(2)}},
	}
	mux := http.NewServeMux()
	(&Handler{Neo4j: graph, Instruments: inst}).Mount(mux)
	if w := post(mux, `{"runtime":"node","major_below":20}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}

	metrics := collect(t, reader)
	duration, ok := metrics["eshu_dp_runtime_declaration_query_duration_seconds"]
	if !ok {
		t.Fatal("duration histogram was not recorded")
	}
	hist := duration.Data.(metricdata.Histogram[float64])
	operations := map[string]bool{}
	for _, point := range hist.DataPoints {
		op, _ := point.Attributes.Value("operation")
		outcome, _ := point.Attributes.Value("outcome")
		if outcome.AsString() != telemetry.RuntimeDeclarationOutcomeOK {
			t.Errorf("outcome = %q, want ok", outcome.AsString())
		}
		operations[op.AsString()] = true
	}
	for _, want := range []string{
		telemetry.RuntimeDeclarationOperationList,
		telemetry.RuntimeDeclarationOperationExcludedCount,
		telemetry.RuntimeDeclarationOperationRepositoryNames,
	} {
		if !operations[want] {
			t.Errorf("no duration recorded for operation %q; got %v", want, operations)
		}
	}

	excluded, ok := metrics["eshu_dp_runtime_declaration_query_excluded_total"]
	if !ok {
		t.Fatal("excluded counter was not recorded")
	}
	byReason := map[string]int64{}
	for _, point := range excluded.Data.(metricdata.Sum[int64]).DataPoints {
		reason, _ := point.Attributes.Value("reason")
		byReason[reason.AsString()] = point.Value
	}
	if byReason["range"] != 3 || byReason["alias"] != 2 || len(byReason) != 2 {
		t.Errorf("excluded by reason = %v", byReason)
	}
}

func TestListRecordsErrorOutcomeOnAFailedRead(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	mux := http.NewServeMux()
	(&Handler{Neo4j: &fakeGraph{pageErr: stubError{}}, Instruments: inst}).Mount(mux)
	post(mux, `{}`)

	hist := collect(t, reader)["eshu_dp_runtime_declaration_query_duration_seconds"].Data.(metricdata.Histogram[float64])
	if len(hist.DataPoints) != 1 {
		t.Fatalf("data points = %d, want 1", len(hist.DataPoints))
	}
	if outcome, _ := hist.DataPoints[0].Attributes.Value("outcome"); outcome.AsString() != telemetry.RuntimeDeclarationOutcomeError {
		t.Errorf("outcome = %q, want error", outcome.AsString())
	}
}
