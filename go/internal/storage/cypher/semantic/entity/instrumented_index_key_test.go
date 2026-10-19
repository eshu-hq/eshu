// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// oversizedIndexValue is above both Neo4j limits measured for #7058 (8164
// bytes for a single string key, 8151 for a (string, string, int) key).
var oversizedIndexValue = strings.Repeat("v", 9000)

func newIndexKeyTestInstruments(t *testing.T) (*telemetry.Instruments, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// metricCounterValue duplicates the metric-reading helper from package
// cypher (reconciliation_drift_metrics_test.go). Test helpers cannot be
// imported across the package split, so the entity leaf carries its own copy.
func metricCounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	attrKey string,
	attrValue string,
) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 sum", name, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == attrKey && attr.Value.AsString() == attrValue {
						return point.Value
					}
				}
			}
		}
	}
	t.Fatalf("metric %s with %s=%q not found", name, attrKey, attrValue)
	return 0
}

func oversizedSkips(t *testing.T, reader *metric.ManualReader, label string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return metricCounterValue(t, rm, "eshu_dp_graph_oversized_index_keys_skipped_total", telemetry.MetricDimensionNodeLabel, label)
}

// statementsCarry reports whether any recorded statement parameter still
// holds needle.
func statementsCarry(t *testing.T, stmts []sourcecypher.Statement, needle string) bool {
	t.Helper()
	for _, stmt := range stmts {
		encoded, err := json.Marshal(stmt.Parameters)
		if err != nil {
			t.Fatalf("marshal parameters: %v", err)
		}
		if strings.Contains(string(encoded), needle) {
			return true
		}
	}
	return false
}

func recordedStatements(r *recordingGroupExecutor) []sourcecypher.Statement {
	all := append([]sourcecypher.Statement(nil), r.executeCalls...)
	for _, group := range r.groupCalls {
		all = append(all, group...)
	}
	return all
}

func semanticIndexKeyWrite() semantic.EntityWrite {
	row := func(id, typ, name string) semantic.EntityRow {
		return semantic.EntityRow{
			RepoID: "repo-1", EntityID: id, EntityType: typ, EntityName: name,
			FilePath: "/repo/src/a.ts", RelativePath: "src/a.ts", Language: "typescript",
			StartLine: 1, EndLine: 2,
		}
	}
	return semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []semantic.EntityRow{
			row("fn-ok", "Function", "render"),
			row("fn-big", "Function", oversizedIndexValue),
			row("ann-big", "Annotation", oversizedIndexValue),
			row("var-big", "Variable", oversizedIndexValue),
		},
	}
}

// TestSemanticEntityWriterSkipsOversizedIndexKeysInEveryWriteMode is the F1
// regression: the reducer's semantic-entity writer (legacy rows on Neo4j,
// canonical-node rows on NornicDB) MERGEs Function, Annotation, Variable and
// other labels from facts, outside the canonical materialization guard. Every
// write mode must reach the backend without the oversized rows once it runs
// through sourcecypher.InstrumentedExecutor, as it does in production.
func TestSemanticEntityWriterSkipsOversizedIndexKeysInEveryWriteMode(t *testing.T) {
	modes := map[string]func(sourcecypher.Executor, int) *SemanticEntityWriter{
		"legacy_rows":         NewSemanticEntityWriter,
		"parameterized_rows":  NewSemanticEntityWriterWithParameterizedRows,
		"batched_properties":  NewSemanticEntityWriterWithBatchedProperties,
		"merge_first_rows":    NewSemanticEntityWriterWithMergeFirstRows,
		"canonical_node_rows": NewSemanticEntityWriterWithCanonicalNodeRows,
	}
	for name, newWriter := range modes {
		t.Run(name, func(t *testing.T) {
			instruments, reader := newIndexKeyTestInstruments(t)
			recorder := &recordingGroupExecutor{}
			writer := newWriter(&sourcecypher.InstrumentedExecutor{Inner: recorder, Instruments: instruments}, 0)

			if _, err := writer.WriteSemanticEntities(context.Background(), semanticIndexKeyWrite()); err != nil {
				t.Fatalf("WriteSemanticEntities() error = %v", err)
			}
			stmts := recordedStatements(recorder)
			if statementsCarry(t, stmts, oversizedIndexValue) {
				t.Fatal("an oversized semantic entity value reached the backend")
			}
			if !statementsCarry(t, stmts, `"render"`) {
				t.Fatal("the normal Function row is missing from the write")
			}
			for _, label := range []string{"Function", "Annotation", "Variable"} {
				if got := oversizedSkips(t, reader, label); got != 1 {
					t.Errorf("skips{node_label=%s} = %d, want 1", label, got)
				}
			}
		})
	}
}
