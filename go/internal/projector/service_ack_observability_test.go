package projector

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

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestServiceRunLogsAckFailureWithQueueContext(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-ack",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-ack",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-ack",
			GenerationID: "generation-ack",
			ObservedAt:   time.Date(2026, time.April, 16, 10, 0, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 16, 10, 0, 1, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("projector-ack"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore: &stubFactStore{
			facts: []facts.Envelope{{
				FactID:       "fact-ack",
				ScopeID:      "scope-ack",
				GenerationID: "generation-ack",
				FactKind:     "source_node",
			}},
		},
		Runner: &stubProjectionRunner{
			result: Result{
				ScopeID:      "scope-ack",
				GenerationID: "generation-ack",
			},
		},
		WorkSink:    &stubProjectorWorkSink{ackErr: errors.New("ack store unavailable")},
		Wait:        func(context.Context, time.Duration) error { return context.Canceled },
		Logger:      logger,
		Instruments: instruments,
	}

	err = service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ack projector work") {
		t.Fatalf("Run() error = %v, want ack projector work", err)
	}

	sink := service.WorkSink.(*stubProjectorWorkSink)
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}

	logOutput := logs.String()
	for _, want := range []string{
		`"msg":"projection ack failed"`,
		`"failure_class":"ack_failure"`,
		`"queue":"projector"`,
		`"status":"ack_failed"`,
		`"scope_id":"scope-ack"`,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs missing %s in %s", want, logOutput)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got := projectorCounterValue(t, rm, "eshu_dp_projections_completed_total", map[string]string{
		"queue":    "projector",
		"status":   "ack_failed",
		"scope_id": work.Scope.ScopeID,
	}); got != 1 {
		t.Fatalf("eshu_dp_projections_completed_total ack_failed value = %d, want 1", got)
	}
}

func projectorCounterValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string, wantAttrs map[string]string) int64 {
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
