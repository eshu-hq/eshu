// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestReducerContentionGateAckFanoutTelemetryLive(t *testing.T) {
	db := openReducerAckFanoutProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	now := time.Now().UTC()
	const scope, generation, id = "repository:6488-telemetry", "generation:6488-telemetry", "work:6488-telemetry"
	seedContainerImageIdentityAckScope(t, ctx, db, scope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scope, generation)
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, scope, generation); err != nil {
		t.Fatal(err)
	}
	insertCrossScopeCompletionBaseConsumer(t, ctx, db, id, scope, generation, reducer.DomainSupplyChainImpact, now)
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running',lease_owner='telemetry-ack',claim_until=$1 WHERE work_item_id=$2`, now.Add(time.Hour), id); err != nil {
		t.Fatal(err)
	}
	observed, reader, spans := ackFanoutTelemetryDB(t, SQLDB{DB: db})
	queue := ReducerQueue{db: observed, LeaseOwner: "telemetry-ack", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
	if err := queue.AckBatch(ctx, []reducer.Intent{{IntentID: id, Domain: reducer.DomainSupplyChainImpact}}, nil); err != nil {
		t.Fatal(err)
	}
	assertCrossScopeConsumerState(t, ctx, db, id, "succeeded", false)
	event := insertCrossScopeCompletionEvent(t, ctx, db, reducer.DomainCICDRunCorrelation, "claimed", "telemetry-fanout", now.Add(time.Hour), 1, now.Add(-10*time.Second))
	lease := reducer.CrossScopeCompletionLease{EventID: event, ProducerDomain: reducer.DomainCICDRunCorrelation, LeaseOwner: "telemetry-fanout", ClaimEpoch: 1, AttemptCount: 1}
	observer := NewQueueObserverStore(SQLDB{DB: db})
	observer.Now = func() time.Time { return now }
	// A separate reader keeps gauge callbacks from adding database timings to the
	// measured ACK/fanout wrapper. Both collectors exercise production emission.
	gaugeReader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(gaugeReader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	if err := telemetry.RegisterObservableGauges(&telemetry.Instruments{}, provider.Meter("ack-fanout-gauges"), observer, nil); err != nil {
		t.Fatal(err)
	}
	assertAckFanoutGauges(t, ctx, gaugeReader, true)
	store := NewCrossScopeCompletionStore(observed)
	store.Now = func() time.Time { return now }
	logRecord := runAckFanoutTelemetryRunner(t, &ackFanoutTelemetryQueue{CrossScopeCompletionQueue: store, lease: lease})
	if logRecord["msg"] != "cross-scope completion fanout committed" || logRecord["producer_domain"] != string(lease.ProducerDomain) || logRecord["events_processed"] != float64(1) || logRecord["producer_items_processed"] != float64(1) || logRecord["intents_enqueued"] != float64(1) {
		t.Fatalf("committed log=%v", logRecord)
	}
	if duration, ok := logRecord["fanout_duration_ms"].(float64); !ok || duration < 0 {
		t.Fatalf("missing nonnegative duration: %v", logRecord)
	}
	assertCrossScopeConsumerState(t, ctx, db, id, "pending", false)
	assertAckFanoutGauges(t, ctx, gaugeReader, false)
	assertAckFanoutTelemetry(t, ctx, reader, spans, false)
	t.Logf("actual ACK/fanout emitted reducer write/read durations and spans; completion gauges drained; log=%v", logRecord)
}

// Controlled synchronous errors prove instrumentation and wrapped runner error
// emission. They are not another deadlock reproduction or hosted export proof.
func TestReducerContentionGateAckFanoutTelemetryErrors(t *testing.T) {
	cause := &pgconn.PgError{Code: "40P01", Message: "controlled telemetry failure"}
	observed, reader, spans := ackFanoutTelemetryDB(t, &instrumentedTestExecQueryer{execErr: cause, queryErr: cause})
	queue := ReducerQueue{db: observed, LeaseOwner: "telemetry-ack", LeaseDuration: time.Minute}
	err := queue.AckBatch(t.Context(), []reducer.Intent{{IntentID: "telemetry-error", Domain: reducer.DomainSupplyChainImpact}}, nil)
	if !errors.Is(err, cause) {
		t.Fatalf("ACK error=%v", err)
	}
	store := NewCrossScopeCompletionStore(observed)
	lease := reducer.CrossScopeCompletionLease{EventID: 17, ProducerDomain: reducer.DomainCICDRunCorrelation, LeaseOwner: "telemetry-fanout", ClaimEpoch: 3, AttemptCount: 2}
	adapter := &ackFanoutTelemetryQueue{CrossScopeCompletionQueue: store, lease: lease}
	record := runAckFanoutTelemetryRunner(t, adapter)
	if record["msg"] != "cross-scope completion fanout failed" {
		t.Fatalf("failure log=%v", record)
	}
	failure, ok := record["error"].(string)
	if !ok {
		t.Fatalf("missing failure: %v", record)
	}
	for _, fragment := range []string{"producer_domain=ci_cd_run_correlation", "event_id=17", "claim_epoch=3", "attempt=2", "40P01", "controlled telemetry failure"} {
		if !strings.Contains(failure, fragment) {
			t.Errorf("failure lacks %q: %s", fragment, failure)
		}
	}
	if !errors.Is(adapter.retryCause, cause) {
		t.Fatalf("retry cause=%v", adapter.retryCause)
	}
	assertAckFanoutTelemetry(t, t.Context(), reader, spans, true)
	t.Logf("controlled synchronous SQLSTATE 40P01 emitted both error spans, exception events and durations; log=%v", record)
}

func ackFanoutTelemetryDB(t *testing.T, inner ExecQueryer) (*InstrumentedDB, *sdkmetric.ManualReader, *tracetest.SpanRecorder) {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = tracer.Shutdown(context.Background()); _ = meter.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(meter.Meter("ack-fanout-proof"))
	if err != nil {
		t.Fatal(err)
	}
	return &InstrumentedDB{Inner: inner, Tracer: tracer.Tracer("ack-fanout-proof"), Instruments: instruments, StoreName: "reducer"}, reader, spans
}

func assertAckFanoutTelemetry(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader, recorder *tracetest.SpanRecorder, wantError bool) {
	t.Helper()
	seen := map[string]bool{}
	for _, span := range recorder.Ended() {
		if span.Name() != "postgres.exec" && span.Name() != "postgres.query" {
			continue
		}
		attrs := attribute.NewSet(span.Attributes()...)
		value, ok := attrs.Value("eshu.store")
		if !ok || value.AsString() != "reducer" {
			t.Fatalf("span store=%v", attrs)
		}
		if (span.Status().Code == codes.Error) != wantError {
			t.Fatalf("span %s status=%v", span.Name(), span.Status())
		}
		if wantError {
			found := false
			for _, event := range span.Events() {
				if event.Name == "exception" {
					found = true
				}
			}
			if !found {
				t.Fatalf("span %s lacks exception", span.Name())
			}
		}
		seen[span.Name()] = true
	}
	if !seen["postgres.exec"] || !seen["postgres.query"] {
		t.Fatalf("missing ACK/fanout spans: %v", seen)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	operations := map[string]bool{}
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "eshu_dp_postgres_query_duration_seconds" {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("duration type=%T", metric.Data)
			}
			for _, point := range histogram.DataPoints {
				store, _ := point.Attributes.Value("store")
				operation, _ := point.Attributes.Value("operation")
				if store.AsString() != "reducer" || point.Count == 0 || point.Sum <= 0 {
					t.Fatalf("invalid duration point=%+v", point)
				}
				operations[operation.AsString()] = true
			}
		}
	}
	if !operations["read"] || !operations["write"] {
		t.Fatalf("missing duration operations=%v", operations)
	}
}

// Claim supplies the seeded lease once. Fanout is the real store call; Retry is
// captured to keep the controlled-error test independent of retry SQL emission.
type ackFanoutTelemetryQueue struct {
	reducer.CrossScopeCompletionQueue
	lease      reducer.CrossScopeCompletionLease
	claimed    bool
	retryCause error
}

func (q *ackFanoutTelemetryQueue) Claim(context.Context, string, time.Duration) (reducer.CrossScopeCompletionLease, bool, error) {
	if q.claimed {
		return reducer.CrossScopeCompletionLease{}, false, nil
	}
	q.claimed = true
	return q.lease, true, nil
}

func (q *ackFanoutTelemetryQueue) Retry(_ context.Context, _ reducer.CrossScopeCompletionLease, cause error, _ time.Time) error {
	q.retryCause = cause
	return nil
}

func runAckFanoutTelemetryRunner(t *testing.T, queue reducer.CrossScopeCompletionQueue) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var output bytes.Buffer
	runner := reducer.CrossScopeCompletionRunner{
		Queue: queue, LeaseOwner: "telemetry-fanout", LeaseTTL: time.Hour,
		Logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Wait:   func(context.Context, time.Duration) error { cancel(); return context.Canceled },
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("runner JSON=%q: %v", output.String(), err)
	}
	return record
}

func assertAckFanoutGauges(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader, queued bool) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	var depth int64
	var age float64
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			switch metric.Name {
			case "eshu_dp_queue_depth":
				gauge, ok := metric.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("depth type=%T", metric.Data)
				}
				for _, point := range gauge.DataPoints {
					queue, _ := point.Attributes.Value("queue")
					if queue.AsString() == "cross_scope_completion.ci_cd_run_correlation" {
						depth += point.Value
					}
				}
			case "eshu_dp_queue_oldest_age_seconds":
				gauge, ok := metric.Data.(metricdata.Gauge[float64])
				if !ok {
					t.Fatalf("age type=%T", metric.Data)
				}
				for _, point := range gauge.DataPoints {
					queue, _ := point.Attributes.Value("queue")
					if queue.AsString() == "cross_scope_completion.ci_cd_run_correlation" {
						age = point.Value
					}
				}
			}
		}
	}
	if queued && (depth != 1 || age < 9) {
		t.Fatalf("queued depth=%d age=%f", depth, age)
	}
	if !queued && (depth != 0 || age != 0) {
		t.Fatalf("drained depth=%d age=%f", depth, age)
	}
	t.Logf("completion gauge queued=%t depth=%d age_seconds=%f", queued, depth, age)
}
