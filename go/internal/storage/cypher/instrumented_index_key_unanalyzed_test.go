// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// unanalyzedShape writes a Parameter key from a nested UNWIND element, a
// shape the index-key analyzer cannot measure (#7058 review N1).
const unanalyzedShape = "UNWIND $rows AS row\nUNWIND row.params AS p\nMERGE (x:Parameter {name: p.name, path: row.file_path, function_line_number: 1})"

// unanalyzedCount sums the unanalyzed counter points carrying attrKey=attrValue.
// It returns 0 when the counter has no such point.
func unanalyzedCount(t *testing.T, reader *sdkmetric.ManualReader, attrKey, attrValue string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok || record.Name != "eshu_dp_graph_index_key_guard_unanalyzed_total" {
				continue
			}
			for _, point := range sum.DataPoints {
				if v, ok := point.Attributes.Value(attribute.Key(attrKey)); ok && v.AsString() == attrValue {
					total += point.Value
				}
			}
		}
	}
	return total
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// TestInstrumentedExecutorReportsUnanalyzedIndexWrites proves the fail-loud
// path: a write to a schema-indexed label in a shape the analyzer cannot read
// still executes with its rows intact, is counted on every attempt, and is
// logged once per distinct statement without parameter values.
func TestInstrumentedExecutorReportsUnanalyzedIndexWrites(t *testing.T) {
	instruments, reader := newIndexKeyTestInstruments(t)
	logs := captureWarnings(t)
	recorder := &recordingGroupExecutor{}
	exec := &InstrumentedExecutor{Inner: recorder, Instruments: instruments}

	secret := "sensitive-parameter-value"
	stmt := Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    unanalyzedShape + "\n// distinct-statement-marker",
		Parameters: map[string]any{"rows": []map[string]any{
			{"file_path": secret, "params": []any{map[string]any{"name": "p"}}},
		}},
	}
	for range 2 {
		if err := exec.Execute(context.Background(), stmt); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	}

	if len(recorder.executeCalls) != 2 {
		t.Fatalf("executed %d statements, want both (the loud path reports, it does not drop)", len(recorder.executeCalls))
	}
	if got := unanalyzedCount(t, reader, "node_label", "Parameter"); got != 2 {
		t.Errorf("unanalyzed{node_label=Parameter} = %d, want 2 (one per attempt)", got)
	}
	if got := unanalyzedCount(t, reader, "reason", "unresolved_value"); got != 2 {
		t.Errorf("unanalyzed{reason=unresolved_value} = %d, want 2", got)
	}
	out := logs.String()
	if n := strings.Count(out, "graph write shape not analyzed by the index-key guard"); n != 1 {
		t.Fatalf("WARN count = %d, want exactly 1 per distinct statement:\n%s", n, out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("WARN leaked a parameter value:\n%s", out)
	}
	if !strings.Contains(out, "node_label=Parameter") || !strings.Contains(out, "reason=unresolved_value") {
		t.Fatalf("WARN misses the label or reason:\n%s", out)
	}
}

func TestInstrumentedExecutorStatementHeadIsBounded(t *testing.T) {
	logs := captureWarnings(t)
	recorder := &recordingGroupExecutor{}
	exec := &InstrumentedExecutor{Inner: recorder}
	long := "UNWIND $rows AS row\nUNWIND row.params AS p\nMERGE (x:Parameter {name: p.name, path: row.file_path, function_line_number: 1}) // " + strings.Repeat("z", 600)
	if err := exec.Execute(context.Background(), Statement{Cypher: long, Parameters: map[string]any{"rows": []map[string]any{}}}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Contains(logs.String(), strings.Repeat("z", 100)) {
		t.Fatalf("WARN carries an unbounded statement:\n%s", logs.String())
	}
}

func TestInstrumentedExecutorAnalyzedWriteIsNotReported(t *testing.T) {
	instruments, reader := newIndexKeyTestInstruments(t)
	logs := captureWarnings(t)
	exec := &InstrumentedExecutor{Inner: &recordingGroupExecutor{}, Instruments: instruments}
	stmt := Statement{
		Cypher:     "UNWIND $rows AS row\nMERGE (m:Module {name: row.name})",
		Parameters: map[string]any{"rows": []map[string]any{{"name": "fmt"}}},
	}
	if err := exec.Execute(context.Background(), stmt); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := unanalyzedCount(t, reader, "node_label", "Module"); got != 0 {
		t.Errorf("recognized shape counted as unanalyzed: %d", got)
	}
	if strings.Contains(logs.String(), "not analyzed") {
		t.Fatalf("recognized shape logged:\n%s", logs.String())
	}
}
