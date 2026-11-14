// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	telemetry "github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestBatchWorkerOccupancyReportsOccupiedWorkerWithIntentAndAge is the #10480
// failing-first gate: while worker 1 executes an intent, a snapshot must read
// occupied with that intent's domain and id plus a positive execution age,
// and worker 0 must read idle. Telemetry only observes; the tracker never
// alters scheduling, locking, or claim flow.
func TestBatchWorkerOccupancyReportsOccupiedWorkerWithIntentAndAge(t *testing.T) {
	t.Parallel()

	intent := makeTestIntents(1)[0]
	occupancy := newBatchWorkerOccupancy(2)
	start := time.Now().UTC()
	occupancy.occupy(1, intent)

	snapshot := occupancy.snapshot(start.Add(90 * time.Second))

	if len(snapshot) != 2 {
		t.Fatalf("snapshot workers = %d, want 2", len(snapshot))
	}
	idle := snapshot[0]
	if idle.Busy {
		t.Fatalf("worker 0 busy = true, want idle")
	}
	occupied := snapshot[1]
	if !occupied.Busy {
		t.Fatal("worker 1 busy = false, want occupied while executing")
	}
	if occupied.Domain != string(intent.Domain) {
		t.Fatalf("worker 1 domain = %q, want %q", occupied.Domain, string(intent.Domain))
	}
	if occupied.IntentID != intent.IntentID {
		t.Fatalf("worker 1 intent = %q, want %q", occupied.IntentID, intent.IntentID)
	}
	if occupied.ExecutionAgeSeconds <= 89 || occupied.ExecutionAgeSeconds > 90 {
		t.Fatalf("worker 1 execution age = %v, want within (89, 90]", occupied.ExecutionAgeSeconds)
	}
}

// TestBatchWorkerOccupancyReleasesWorkerAfterExecution proves a worker reads
// idle again once its execution reports, so the gauge and the start log
// cannot wedge busy after the run drains.
func TestBatchWorkerOccupancyReleasesWorkerAfterExecution(t *testing.T) {
	t.Parallel()

	intent := makeTestIntents(1)[0]
	occupancy := newBatchWorkerOccupancy(1)
	occupancy.occupy(0, intent)
	occupancy.release(0)

	snapshot := occupancy.snapshot(time.Now().UTC())
	if len(snapshot) != 1 {
		t.Fatalf("snapshot workers = %d, want 1", len(snapshot))
	}
	if snapshot[0].Busy {
		t.Fatal("worker 0 busy = true after release, want idle")
	}
	if snapshot[0].ExecutionAgeSeconds != 0 {
		t.Fatalf("worker 0 execution age = %v, want 0 while idle", snapshot[0].ExecutionAgeSeconds)
	}
}

// blockingOccupancyExecutor parks inside Execute until released, reproducing
// the #10480 stuck-worker shape: the claim loop parks while every worker sits
// inside executeAndReport.
type blockingOccupancyExecutor struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *blockingOccupancyExecutor) Execute(_ context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	e.once.Do(func() { close(e.entered) })
	<-e.release
	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   ResultStatusSucceeded,
	}, nil
}

// TestRunBatchConcurrentExposesOccupiedWorkerDuringExecution proves the
// instrumented batch path admits work identically (same claims, same acks)
// while an in-flight execution reads occupied with its intent and a growing
// age — the 3 AM signal for a worker stuck inside executeAndReport.
func TestRunBatchConcurrentExposesOccupiedWorkerDuringExecution(t *testing.T) {
	t.Parallel()

	executor := &blockingOccupancyExecutor{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	source := &fakeBatchWorkSource{intents: makeTestIntents(1)}
	sink := &fakeBatchWorkSink{}
	service := Service{
		PollInterval:   time.Millisecond,
		WorkSource:     source,
		Executor:       executor,
		WorkSink:       sink,
		Workers:        1,
		BatchClaimSize: 1,
		Wait:           func(context.Context, time.Duration) error { return context.Canceled },
	}

	// Size the tracker before the run, mirroring production wiring
	// (InitBatchWorkerOccupancy in cmd/reducer): the run copies the Service
	// by value, so the tracker must already be shared, not lazily created
	// on the running copy.
	service.InitBatchWorkerOccupancy()

	runErr := make(chan error, 1)
	go func() {
		runErr <- service.runBatchConcurrent(context.Background(), source, sink)
	}()

	var occupied telemetry.BatchWorkerOccupancy
	select {
	case <-executor.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker never entered execution")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		snapshot, err := service.BatchWorkerOccupancy(context.Background())
		if err != nil {
			t.Fatalf("BatchWorkerOccupancy() error = %v", err)
		}
		if len(snapshot) == 1 && snapshot[0].Busy {
			occupied = snapshot[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker never read occupied while inside execution")
		}
		time.Sleep(time.Millisecond)
	}
	if occupied.IntentID != "intent-0" {
		t.Fatalf("occupied intent = %q, want intent-0", occupied.IntentID)
	}
	if occupied.ExecutionAgeSeconds < 0 {
		t.Fatalf("occupied execution age = %v, want >= 0", occupied.ExecutionAgeSeconds)
	}

	close(executor.release)
	// The empty poll after the drain hits Wait, which fails closed, so the
	// run returns and the acker flushes the single success.
	if err := <-runErr; err != nil {
		t.Fatalf("runBatchConcurrent() error = %v, want nil", err)
	}
	if sink.acked != 1 {
		t.Fatalf("acked = %d, want 1 (instrumented path admits identically)", sink.acked)
	}
	snapshot, err := service.BatchWorkerOccupancy(context.Background())
	if err != nil {
		t.Fatalf("BatchWorkerOccupancy() error = %v", err)
	}
	if len(snapshot) == 1 && snapshot[0].Busy {
		t.Fatal("worker busy after run drained, want idle")
	}
}

// TestExecuteAndReportRunSpanCarriesWorkerIntentDomain proves the
// instrumented batch execution starts the existing reducer.run span with the
// worker, intent, and domain attributes, so an execution stuck inside
// executeAndReport shows what it is executing in tracing backends.
func TestExecuteAndReportRunSpanCarriesWorkerIntentDomain(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	service := Service{
		Executor:       &countingExecutor{},
		WorkSink:       &fakeBatchWorkSink{},
		Tracer:         provider.Tracer("test"),
		batchOccupancy: newBatchWorkerOccupancy(2),
	}
	intent := makeTestIntents(1)[0]

	if _, needsAck, err := service.executeAndReportTracked(context.Background(), intent, 1); err != nil || !needsAck {
		t.Fatalf("executeAndReportTracked() = (_, %v, %v), want (_, true, nil)", needsAck, err)
	}

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1 reducer.run span", len(ended))
	}
	if ended[0].Name() != string(telemetry.SpanReducerRun) {
		t.Fatalf("span name = %q, want %q", ended[0].Name(), telemetry.SpanReducerRun)
	}
	wantAttrs := map[string]string{
		"worker_id": "1",
		"intent_id": intent.IntentID,
		"domain":    string(intent.Domain),
	}
	for _, attr := range ended[0].Attributes() {
		if want, ok := wantAttrs[string(attr.Key)]; ok {
			if attr.Value.AsString() != want {
				t.Fatalf("span attr %s = %q, want %q", attr.Key, attr.Value.AsString(), want)
			}
			delete(wantAttrs, string(attr.Key))
		}
	}
	if len(wantAttrs) != 0 {
		t.Fatalf("span missing attributes: %v", wantAttrs)
	}
}

// TestExecuteAndReportTrackedLogsExecutionStart proves the instrumented batch
// wrapper names the worker, the intent, and the domain on the existing log
// family before the handler runs, so the intent id of a stuck execution is
// recoverable from logs alone.
func TestExecuteAndReportTrackedLogsExecutionStart(t *testing.T) {
	t.Parallel()

	var logs strings.Builder
	service := Service{
		Executor:       &countingExecutor{},
		WorkSink:       &fakeBatchWorkSink{},
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		batchOccupancy: newBatchWorkerOccupancy(2),
	}
	intent := makeTestIntents(1)[0]

	if _, _, err := service.executeAndReportTracked(context.Background(), intent, 1); err != nil {
		t.Fatalf("executeAndReportTracked() error = %v, want nil", err)
	}

	line := logs.String()
	if !strings.Contains(line, `"msg":"reducer batch worker execution started"`) {
		t.Fatalf("start log missing worker execution started line: %s", line)
	}
	if !strings.Contains(line, `"worker_id":"1"`) {
		t.Fatalf("start log missing worker_id 1: %s", line)
	}
	if !strings.Contains(line, `"intent_id":"intent-0"`) {
		t.Fatalf("start log missing intent-0: %s", line)
	}
}
