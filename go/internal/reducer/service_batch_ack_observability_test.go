// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type observingBatchSink struct {
	ackErr    error
	ackCalls  int
	ackSizes  []int
	failCalls int
}

func (*observingBatchSink) Ack(context.Context, reducercontract.Intent, reducercontract.Result) error {
	return nil
}

func (s *observingBatchSink) AckBatch(_ context.Context, intents []reducercontract.Intent, _ []reducercontract.Result) error {
	s.ackCalls++
	s.ackSizes = append(s.ackSizes, len(intents))
	return s.ackErr
}

func (s *observingBatchSink) Fail(context.Context, reducercontract.Intent, error) error {
	s.failCalls++
	return nil
}

// cancelGatedBatchWorkSource blocks its first ClaimBatch until the run context
// ends, then hands back the staged intents once: the claim deterministically
// lands after the cancel signal, so the test proves the no-success-after-cancel
// contract without racing the scheduler. Cancelling from inside Execute instead
// races the handler-completion select in executeHandler and can abandon an
// already-succeeded item into Fail (flake #8623).
type cancelGatedBatchWorkSource struct {
	mu       sync.Mutex
	intents  []reducercontract.Intent
	entered  chan struct{}
	signaled bool
	returned bool
}

func (f *cancelGatedBatchWorkSource) Claim(ctx context.Context) (reducercontract.Intent, bool, error) {
	<-ctx.Done()
	return reducercontract.Intent{}, false, nil
}

func (f *cancelGatedBatchWorkSource) ClaimBatch(ctx context.Context, limit int) ([]reducercontract.Intent, error) {
	f.mu.Lock()
	if !f.signaled {
		f.signaled = true
		close(f.entered)
	}
	f.mu.Unlock()

	<-ctx.Done()

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.returned {
		return nil, nil
	}
	f.returned = true
	if limit < len(f.intents) {
		batch := make([]reducercontract.Intent, limit)
		copy(batch, f.intents[:limit])
		f.intents = f.intents[limit:]
		return batch, nil
	}
	batch := f.intents
	f.intents = nil
	return batch, nil
}

func batchTelemetry(t *testing.T) (*telemetry.Instruments, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("reducer-batch-ack"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

func batchMetricStatusCounts(t *testing.T, reader *metric.ManualReader) (map[string]int64, metricdata.ResourceMetrics) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counts := make(map[string]int64)
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_executions_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("reducer executions data = %T, want counter", m.Data)
			}
			for _, point := range sum.DataPoints {
				status, ok := point.Attributes.Value(attribute.Key("status"))
				if !ok {
					t.Fatal("reducer executions missing status label")
				}
				counts[status.AsString()] += point.Value
			}
		}
	}
	return counts, rm
}

func TestServiceBatchAckOutcomeRecordedAfterAck(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		ackErr     error
		wantStatus string
		wantError  bool
	}{
		{name: "partial stale claim", ackErr: ErrExecutionClaimRejected, wantStatus: "ack_outcome_unknown"},
		{name: "infrastructure error", ackErr: errors.New("batch ack unavailable"), wantStatus: "ack_outcome_unknown", wantError: true},
		{name: "success", wantStatus: "succeeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instruments, reader := batchTelemetry(t)
			intents := makeTestIntents(2)
			sink := &observingBatchSink{ackErr: tc.ackErr}
			var logs bytes.Buffer
			service := Service{
				PollInterval:   time.Millisecond,
				WorkSource:     &fakeBatchWorkSource{intents: intents},
				Executor:       &countingExecutor{},
				WorkSink:       sink,
				Workers:        2,
				BatchClaimSize: 2,
				Wait:           func(context.Context, time.Duration) error { return context.Canceled },
				Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
				Instruments:    instruments,
			}

			err := service.runMainLoop(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("runMainLoop() error = %v, wantError %v", err, tc.wantError)
			}
			if sink.ackCalls != 1 || len(sink.ackSizes) != 1 || sink.ackSizes[0] != 2 || sink.failCalls != 0 {
				t.Fatalf("AckBatch calls/sizes = %d/%v, Fail calls = %d, want one two-item ACK and no Fail", sink.ackCalls, sink.ackSizes, sink.failCalls)
			}
			counts, rm := batchMetricStatusCounts(t, reader)
			if counts[tc.wantStatus] != 2 || len(counts) != 1 {
				t.Fatalf("reducer execution statuses = %v, want only %s:2", counts, tc.wantStatus)
			}
			if tc.ackErr != nil {
				if strings.Contains(logs.String(), `"msg":"reducer execution succeeded"`) || !strings.Contains(logs.String(), `"status":"ack_outcome_unknown"`) {
					t.Fatalf("batch ACK error logs falsely report success or omit unknown outcome: %s", logs.String())
				}
			} else if !strings.Contains(logs.String(), `"msg":"reducer execution succeeded"`) {
				t.Fatalf("successful batch ACK logs omit success: %s", logs.String())
			}
			for _, name := range []string{"eshu_dp_reducer_run_duration_seconds", "eshu_dp_reducer_queue_wait_seconds"} {
				if got := reducerHistogramCount(t, rm, name, map[string]string{"domain": string(intents[0].Domain)}); got != 2 {
					t.Fatalf("%s count = %d, want 2", name, got)
				}
			}
		})
	}
}

func TestServiceBatchCanceledBeforeEnqueueHasNoSuccess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instruments, reader := batchTelemetry(t)
	sink := &observingBatchSink{}
	var logs bytes.Buffer
	source := &cancelGatedBatchWorkSource{intents: makeTestIntents(1), entered: make(chan struct{})}
	executor := &countingExecutor{}
	service := Service{
		PollInterval:   time.Millisecond,
		WorkSource:     source,
		Executor:       executor,
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: 1,
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		Instruments:    instruments,
	}

	done := make(chan error, 1)
	go func() { done <- service.runMainLoop(ctx) }()
	// Wait until the claimer is blocked inside ClaimBatch, then fire the
	// cancel: the claim deterministically lands after shutdown, so the
	// claimer must gate it instead of racing the workers (#8623).
	select {
	case <-source.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("claimer did not enter ClaimBatch")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runMainLoop() error = %v, want nil on cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runMainLoop() did not stop after cancellation")
	}
	if sink.ackCalls != 0 || sink.failCalls != 0 {
		t.Fatalf("AckBatch calls = %d, Fail calls = %d, want zero", sink.ackCalls, sink.failCalls)
	}
	if got := executor.count.Load(); got != 0 {
		t.Fatalf("executor calls = %d, want zero: a batch claimed after cancel must never execute", got)
	}
	counts, rm := batchMetricStatusCounts(t, reader)
	if counts["ack_outcome_unknown"] != 1 || len(counts) != 1 {
		t.Fatalf("reducer execution statuses = %v, want only ack_outcome_unknown:1", counts)
	}
	if strings.Contains(logs.String(), `"msg":"reducer execution succeeded"`) || !strings.Contains(logs.String(), `"status":"ack_outcome_unknown"`) {
		t.Fatalf("canceled batch logs falsely report success or omit unknown outcome: %s", logs.String())
	}
	for _, name := range []string{"eshu_dp_reducer_run_duration_seconds", "eshu_dp_reducer_queue_wait_seconds"} {
		if got := reducerHistogramCount(t, rm, name, map[string]string{"domain": string(DomainCodeCallMaterialization)}); got != 1 {
			t.Fatalf("%s count = %d, want 1", name, got)
		}
	}
}
