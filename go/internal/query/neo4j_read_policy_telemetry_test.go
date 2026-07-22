// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNeo4jReaderParentDeadlineDoesNotRecordPolicyDeadlineOutcome(t *testing.T) {
	manualReader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(manualReader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("neo4j-read-policy-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	reader := newPolicyTestNeo4jReader(blockingPolicySession)
	reader.policy.instruments = instruments
	reader.policy.readTimeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, _ = reader.Run(ctx, "RETURN 1", nil)

	var metrics metricdata.ResourceMetrics
	if err := manualReader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := graphReadMetricOutcome(metrics)
	if got != string(graphReadOutcomeCallerDeadline) {
		t.Fatalf("graph read metric outcome = %q, want %q", got, graphReadOutcomeCallerDeadline)
	}
	if got == string(graphReadOutcomeDeadline) {
		t.Fatal("parent deadline incremented the graph-policy deadline outcome")
	}
}

func TestNeo4jReaderSpansDoNotExposeQueryText(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{{
			Keys: []string{"secret"}, Values: []any{"redacted"},
		}}}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")
	queryText := "RETURN 'private-query-marker' AS secret"

	if _, err := reader.Run(context.Background(), queryText, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, err := reader.RunSingle(context.Background(), queryText, nil); err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}

	for _, span := range recorder.Ended() {
		for _, field := range span.Attributes() {
			if strings.Contains(field.Value.Emit(), "private-query-marker") {
				t.Fatalf("span %q attribute %q exposed query text", span.Name(), field.Key)
			}
		}
	}
}

func TestNeo4jReaderWarningDoesNotExposeQueryOrDriverCause(t *testing.T) {
	const (
		queryText    = "MATCH (secret:PrivateThing) RETURN secret"
		privateCause = "bolt://private.example.invalid:7687"
	)
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{run: func(
			context.Context,
			string,
			map[string]any,
			...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			return nil, &neo4jdriver.ConnectivityError{Inner: errors.New(privateCause)}
		}}
	})
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	_, _ = reader.Run(context.Background(), queryText, nil)
	if got := logs.String(); strings.Contains(got, queryText) || strings.Contains(got, privateCause) {
		t.Fatalf("warning exposed query or driver cause: %s", got)
	}
}

func TestNeo4jReaderSessionCleanupFailureIsObservableAndSanitized(t *testing.T) {
	const privateCause = "bolt://private-cleanup.example.invalid:7687"
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{
			result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}},
			close:  func(context.Context) error { return errors.New(privateCause) },
		}
	})
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := logs.String()
	if !strings.Contains(got, `"event_name":"query.graph_read.session_close_failed"`) ||
		!strings.Contains(got, `"failure_class":"session_close_error"`) {
		t.Fatalf("cleanup warning = %s, want bounded event and failure class", got)
	}
	if strings.Contains(got, privateCause) {
		t.Fatalf("cleanup warning exposed driver cause: %s", got)
	}
}

func TestNeo4jReaderRecoveredReadAnnotatesBoundedSpanOutcome(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		if attempts == 1 {
			return &fakeNeo4jReadSession{run: connectivityErrorRun("temporary disconnect")}
		}
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := graphReadSpanString(spans[0].Attributes(), telemetry.SpanAttrGraphReadOutcome); got != string(graphReadOutcomeRecovered) {
		t.Fatalf("graph read outcome = %q, want %q", got, graphReadOutcomeRecovered)
	}
	if got := graphReadSpanInt(spans[0].Attributes(), telemetry.SpanAttrGraphReadAttempts); got != maxGraphReadAttempts {
		t.Fatalf("graph read attempts = %d, want %d", got, maxGraphReadAttempts)
	}
}

func TestNeo4jReaderSlowSuccessEmitsBoundedWarning(t *testing.T) {
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}}}
	})
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	reader.policy.slowThreshold = time.Nanosecond

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := logs.String(); !strings.Contains(got, `"failure_class":"slow"`) ||
		!strings.Contains(got, `"event_name":"query.graph_read.warning"`) {
		t.Fatalf("slow warning = %s, want bounded outcome and event", got)
	}
}

func graphReadMetricOutcome(metrics metricdata.ResourceMetrics) string {
	for _, scope := range metrics.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_neo4j_query_duration_seconds" {
				continue
			}
			histogram, ok := record.Data.(metricdata.Histogram[float64])
			if !ok || len(histogram.DataPoints) != 1 {
				return ""
			}
			value, _ := histogram.DataPoints[0].Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
			return value.AsString()
		}
	}
	return ""
}

func graphReadSpanString(attributes []attribute.KeyValue, key string) string {
	for _, candidate := range attributes {
		if string(candidate.Key) == key {
			return candidate.Value.AsString()
		}
	}
	return ""
}

func graphReadSpanInt(attributes []attribute.KeyValue, key string) int64 {
	for _, candidate := range attributes {
		if string(candidate.Key) == key {
			return candidate.Value.AsInt64()
		}
	}
	return 0
}
