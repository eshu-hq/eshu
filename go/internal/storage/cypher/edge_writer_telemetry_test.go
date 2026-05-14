package cypher

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestEdgeWriterWriteEdgesLogsSharedWriteShape(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 2)
	writer.Logger = logger

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "entity:function:a", "callee_entity_id": "entity:function:b"}},
		{IntentID: "i2", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "entity:function:c", "callee_entity_id": "entity:function:d"}},
		{IntentID: "i3", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "entity:function:e", "callee_entity_id": "entity:function:f"}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v\nlogs:\n%s", err, logs.String())
	}

	if got, want := entry["msg"], "shared edge write completed"; got != want {
		t.Fatalf("log msg = %v, want %v", got, want)
	}
	if got, want := entry["domain"], reducer.DomainCodeCalls; got != want {
		t.Fatalf("domain = %v, want %v", got, want)
	}
	if got, want := entry["evidence_source"], "parser/code-calls"; got != want {
		t.Fatalf("evidence_source = %v, want %v", got, want)
	}
	if got, want := entry["execution_mode"], "group"; got != want {
		t.Fatalf("execution_mode = %v, want %v", got, want)
	}
	if got, want := entry["input_rows"], float64(3); got != want {
		t.Fatalf("input_rows = %v, want %v", got, want)
	}
	if got, want := entry["written_rows"], float64(3); got != want {
		t.Fatalf("written_rows = %v, want %v", got, want)
	}
	if got, want := entry["skipped_rows"], float64(0); got != want {
		t.Fatalf("skipped_rows = %v, want %v", got, want)
	}
	if got, want := entry["route_count"], float64(1); got != want {
		t.Fatalf("route_count = %v, want %v", got, want)
	}
	if got, want := entry["statement_count"], float64(2); got != want {
		t.Fatalf("statement_count = %v, want %v", got, want)
	}
	if got, want := entry["batch_size"], float64(2); got != want {
		t.Fatalf("batch_size = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesLogsCodeCallRouteSummaries(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 10)
	writer.Logger = logger
	writer.CodeCallGroupBatchSize = 1

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"caller_entity_id":   "repo-a:src/app.ts",
				"caller_entity_type": "File",
				"callee_entity_id":   "entity:component",
				"callee_entity_type": "Function",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v\nlogs:\n%s", err, logs.String())
	}
	rawSummaries, ok := entry["statement_summaries"].([]any)
	if !ok || len(rawSummaries) != 1 {
		t.Fatalf("statement_summaries = %#v, want one route summary", entry["statement_summaries"])
	}
	summary, ok := rawSummaries[0].(string)
	if !ok {
		t.Fatalf("statement summary type = %T, want string", rawSummaries[0])
	}
	for _, want := range []string{
		"domain=code_calls",
		"relationship=CALLS",
		"source=File",
		"target=Function",
		"rows=1",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("statement summary = %q, want fragment %q", summary, want)
		}
	}
}

func TestEdgeWriterWriteEdgesCodeCallIsolationRecordsBatchTelemetry(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 500)
	writer.Instruments = instruments
	writer.CodeCallBatchSize = 2
	writer.CodeCallGroupBatchSize = 1

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "entity:function:a", "callee_entity_id": "entity:function:b"}},
		{IntentID: "i2", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "entity:function:c", "callee_entity_id": "entity:function:d"}},
		{IntentID: "i3", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "entity:function:e", "callee_entity_id": "entity:function:f"}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got, want := int64(len(executor.groupCalls)), int64(2); got != want {
		t.Fatalf("isolated group calls = %d, want %d", got, want)
	}
	wantAttrs := map[string]string{"domain": reducer.DomainCodeCalls}
	assertInt64CounterValue(t, rm, "eshu_dp_code_call_edge_batches_total", wantAttrs, 2)
	assertFloat64HistogramCount(t, rm, "eshu_dp_code_call_edge_batch_duration_seconds", wantAttrs, 2)
}

func TestEdgeWriterWriteEdgesNonCodeCallDoesNotRecordCodeCallBatchTelemetry(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 2)
	writer.Instruments = instruments
	writer.CodeCallBatchSize = 2
	writer.CodeCallGroupBatchSize = 1

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-b"}},
		{IntentID: "i2", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-c"}},
		{IntentID: "i3", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-d"}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	assertMetricMissing(t, rm, "eshu_dp_code_call_edge_batches_total")
	assertMetricMissing(t, rm, "eshu_dp_code_call_edge_batch_duration_seconds")
}

func TestEdgeWriterWriteEdgesInheritanceRecordsSharedGroupTelemetry(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 2)
	writer.Instruments = instruments
	writer.InheritanceGroupBatchSize = 1

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"child_entity_id": "entity:class:a", "parent_entity_id": "entity:class:b", "repo_id": "repo-a", "relationship_type": "INHERITS"}},
		{IntentID: "i2", RepositoryID: "repo-a", Payload: map[string]any{"child_entity_id": "entity:class:c", "parent_entity_id": "entity:class:d", "repo_id": "repo-a", "relationship_type": "INHERITS"}},
		{IntentID: "i3", RepositoryID: "repo-a", Payload: map[string]any{"child_entity_id": "entity:class:e", "parent_entity_id": "entity:class:f", "repo_id": "repo-a", "relationship_type": "INHERITS"}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	wantAttrs := map[string]string{"domain": reducer.DomainInheritanceEdges}
	assertInt64CounterValue(t, rm, "eshu_dp_shared_edge_write_groups_total", wantAttrs, 2)
	assertFloat64HistogramCount(t, rm, "eshu_dp_shared_edge_write_group_duration_seconds", wantAttrs, 2)
	assertInt64HistogramCount(t, rm, "eshu_dp_shared_edge_write_group_statement_count", wantAttrs, 2)
}

func TestEdgeWriterWriteEdgesRepoDependencyRecordsSharedGroupTelemetry(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 2)
	writer.Instruments = instruments

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-b"}},
		{IntentID: "i2", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-c"}},
		{IntentID: "i3", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-d"}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	wantAttrs := map[string]string{"domain": reducer.DomainRepoDependency}
	assertInt64CounterValue(t, rm, "eshu_dp_shared_edge_write_groups_total", wantAttrs, 1)
	assertFloat64HistogramCount(t, rm, "eshu_dp_shared_edge_write_group_duration_seconds", wantAttrs, 1)
	assertInt64HistogramCount(t, rm, "eshu_dp_shared_edge_write_group_statement_count", wantAttrs, 1)
}

func assertInt64CounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
	wantValue int64,
) {
	t.Helper()

	for _, scopeMetric := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetric.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if attributesMatch(point.Attributes, wantAttrs) {
					if got := point.Value; got != wantValue {
						t.Fatalf("metric %s value = %d, want %d", metricName, got, wantValue)
					}
					return
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
}

func assertFloat64HistogramCount(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
	wantCount uint64,
) {
	t.Helper()

	for _, scopeMetric := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetric.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				if attributesMatch(point.Attributes, wantAttrs) {
					if got := point.Count; got != wantCount {
						t.Fatalf("metric %s count = %d, want %d", metricName, got, wantCount)
					}
					return
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
}

func assertInt64HistogramCount(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
	wantCount uint64,
) {
	t.Helper()

	for _, scopeMetric := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetric.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[int64]", metricName, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				if attributesMatch(point.Attributes, wantAttrs) {
					if got := point.Count; got != wantCount {
						t.Fatalf("metric %s count = %d, want %d", metricName, got, wantCount)
					}
					return
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
}

func assertMetricMissing(t *testing.T, rm metricdata.ResourceMetrics, metricName string) {
	t.Helper()

	for _, scopeMetric := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetric.Metrics {
			if metricRecord.Name == metricName {
				t.Fatalf("metric %s unexpectedly recorded", metricName)
			}
		}
	}
}

func attributesMatch(attrs attribute.Set, want map[string]string) bool {
	if len(want) == 0 {
		return len(attrs.ToSlice()) == 0
	}

	gotAttrs := attrs.ToSlice()
	if len(gotAttrs) != len(want) {
		return false
	}
	for _, attr := range gotAttrs {
		wantValue, ok := want[string(attr.Key)]
		if !ok || attr.Value.AsString() != wantValue {
			return false
		}
	}
	return true
}
