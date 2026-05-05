package reducer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestServiceRunLogsAckFailureWithQueueContext(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:        "intent-ack",
		ScopeID:         "scope-ack",
		GenerationID:    "generation-ack",
		SourceSystem:    "git",
		Domain:          DomainRepoDependency,
		Cause:           "typed relationship follow-up",
		EntityKeys:      []string{"repo:eshu"},
		RelatedScopeIDs: []string{"scope-related"},
		EnqueuedAt:      time.Date(2026, time.April, 16, 11, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 16, 11, 0, 0, 0, time.UTC),
		Status:          IntentStatusPending,
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("reducer-ack"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubReducerWorkSource{intents: []Intent{intent}},
		Executor: &stubReducerExecutor{
			result: Result{
				IntentID: intent.IntentID,
				Domain:   intent.Domain,
				Status:   ResultStatusSucceeded,
			},
		},
		WorkSink:    &stubReducerWorkSink{ackErr: errors.New("ack lease update failed")},
		Wait:        func(context.Context, time.Duration) error { return context.Canceled },
		Logger:      logger,
		Instruments: instruments,
	}

	err = service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ack reducer work") {
		t.Fatalf("Run() error = %v, want ack reducer work", err)
	}

	sink := service.WorkSink.(*stubReducerWorkSink)
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}

	logOutput := logs.String()
	for _, want := range []string{
		`"msg":"reducer ack failed"`,
		`"failure_class":"ack_failure"`,
		`"queue":"reducer"`,
		`"status":"ack_failed"`,
		`"intent_id":"intent-ack"`,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs missing %s in %s", want, logOutput)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got := reducerCounterValue(t, rm, "eshu_dp_reducer_executions_total", map[string]string{
		"queue":  "reducer",
		"status": "ack_failed",
		"domain": string(intent.Domain),
	}); got != 1 {
		t.Fatalf("eshu_dp_reducer_executions_total ack_failed value = %d, want 1", got)
	}
}

func TestServiceRunRecordsReducerQueueWait(t *testing.T) {
	t.Parallel()

	availableAt := time.Now().UTC().Add(-2 * time.Minute)
	intent := Intent{
		IntentID:        "intent-wait",
		ScopeID:         "scope-wait",
		GenerationID:    "generation-wait",
		SourceSystem:    "git",
		Domain:          DomainSemanticEntityMaterialization,
		Cause:           "semantic follow-up",
		EntityKeys:      []string{"semantic:repo-a"},
		RelatedScopeIDs: []string{"scope-wait"},
		EnqueuedAt:      availableAt.Add(-30 * time.Second),
		AvailableAt:     availableAt,
		Status:          IntentStatusPending,
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("reducer-wait"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubReducerWorkSource{intents: []Intent{intent}},
		Executor: &stubReducerExecutor{
			result: Result{
				IntentID: intent.IntentID,
				Domain:   intent.Domain,
				Status:   ResultStatusSucceeded,
			},
		},
		WorkSink:    &stubReducerWorkSink{},
		Wait:        func(context.Context, time.Duration) error { return context.Canceled },
		Logger:      logger,
		Instruments: instruments,
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	logOutput := logs.String()
	for _, want := range []string{
		`"msg":"reducer execution succeeded"`,
		`"queue_wait_seconds":`,
		`"handler_duration_seconds":`,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs missing %s in %s", want, logOutput)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got := reducerHistogramCount(t, rm, "eshu_dp_reducer_queue_wait_seconds", map[string]string{
		"domain": string(intent.Domain),
	}); got != 1 {
		t.Fatalf("eshu_dp_reducer_queue_wait_seconds count = %d, want 1", got)
	}
}

func reducerCounterValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string, wantAttrs map[string]string) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, m.Data)
			}

			for _, dp := range sum.DataPoints {
				if hasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func reducerHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string, wantAttrs map[string]string) uint64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}

			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, m.Data)
			}

			for _, dp := range histogram.DataPoints {
				if hasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Count
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func hasAttrs(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}

	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}

	return true
}
