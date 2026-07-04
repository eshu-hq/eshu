// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerAdmissionDefersOnGraphWritePressure proves that a spike of
// retrying-state reducer work (the durable signal of recurring graph-write
// timeouts and retry-exhaustion) defers the producer even when the total
// outstanding depth is well below the total-depth high-water mark. Recoverable
// work therefore stays in the retrying bucket instead of being pushed toward
// retry-exhaustion dead letters.
func TestReducerAdmissionDefersOnGraphWritePressure(t *testing.T) {
	t.Parallel()

	reader := &fakeDepthReader{
		depths: []map[string]map[string]int64{
			// retrying is over the high-water mark; total depth is small.
			{"reducer": {"pending": 2, "retrying": 50}},
			// retrying recovered below the low-water mark; admission resumes.
			{"reducer": {"pending": 2, "retrying": 3}},
		},
	}
	inner := &recordingIntentWriter{}
	sleeps := 0
	admission := writer{
		inner:       inner,
		depthReader: reader,
		config: Config{
			HighWaterMark:         10_000,
			RetryingHighWaterMark: 25,
			RetryingLowWaterMark:  10,
			PollInterval:          time.Second,
		},
		deferral: newDeferralState(),
		sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	}
	intents := []projector.ReducerIntent{{Domain: reducer.DomainWorkloadIdentity}}

	result, err := admission.Enqueue(context.Background(), intents)
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if result.Count != len(intents) {
		t.Fatalf("Enqueue() count = %d, want %d", result.Count, len(intents))
	}
	if got, want := sleeps, 1; got != want {
		t.Fatalf("sleep count = %d, want %d", got, want)
	}
	if got, want := reader.calls, 2; got != want {
		t.Fatalf("depth read count = %d, want %d", got, want)
	}
	if got, want := inner.calls, 1; got != want {
		t.Fatalf("inner enqueue count = %d, want %d", got, want)
	}
}

// TestReducerAdmissionGraphWritePressureHysteresis proves the low-water mark
// holds the producer back through the gap between the high- and low-water
// marks. A reading between the two marks keeps deferring once pressure has
// been detected, so the producer does not flap back on the first partial
// recovery.
func TestReducerAdmissionGraphWritePressureHysteresis(t *testing.T) {
	t.Parallel()

	reader := &fakeDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"retrying": 40}}, // above high-water: defer
			{"reducer": {"retrying": 20}}, // between marks: still defer (hysteresis)
			{"reducer": {"retrying": 8}},  // below low-water: resume
		},
	}
	inner := &recordingIntentWriter{}
	sleeps := 0
	admission := writer{
		inner:       inner,
		depthReader: reader,
		config: Config{
			HighWaterMark:         10_000,
			RetryingHighWaterMark: 25,
			RetryingLowWaterMark:  10,
			PollInterval:          time.Second,
		},
		deferral: newDeferralState(),
		sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	}

	if _, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got, want := sleeps, 2; got != want {
		t.Fatalf("sleep count = %d, want %d (low-water hysteresis must hold)", got, want)
	}
	if got, want := inner.calls, 1; got != want {
		t.Fatalf("inner enqueue count = %d, want %d", got, want)
	}
}

// TestReducerAdmissionGraphWritePressureRecordsReason proves the deferral
// telemetry distinguishes graph-write pressure from total-depth pressure so an
// operator can tell, at 3 AM, whether the backend is slow (graph_write_pressure)
// or the queue is simply deep (high_water).
func TestReducerAdmissionGraphWritePressureRecordsReason(t *testing.T) {
	t.Parallel()

	reader := &fakeDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"retrying": 50}},
			{"reducer": {"retrying": 1}},
		},
	}
	recorder := &recordingDeferralReasonReader{}
	admission := writer{
		inner:       &recordingIntentWriter{},
		depthReader: reader,
		config: Config{
			HighWaterMark:         10_000,
			RetryingHighWaterMark: 25,
			RetryingLowWaterMark:  10,
			PollInterval:          time.Second,
		},
		deferral:         newDeferralState(),
		failureClassSink: recorder.record,
		sleep:            func(context.Context, time.Duration) error { return nil },
	}

	if _, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got, want := recorder.last(), DeferralReasonGraphWritePressure; got != want {
		t.Fatalf("deferral reason = %q, want %q", got, want)
	}
}

// TestReducerAdmissionTotalDepthRecordsHighWaterReason proves the total-depth
// gate still records the original high_water reason, so the new pressure
// signal does not mislabel a deep-but-healthy queue.
func TestReducerAdmissionTotalDepthRecordsHighWaterReason(t *testing.T) {
	t.Parallel()

	reader := &fakeDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"pending": 10}},
			{"reducer": {"pending": 4}},
		},
	}
	recorder := &recordingDeferralReasonReader{}
	admission := writer{
		inner:       &recordingIntentWriter{},
		depthReader: reader,
		config: Config{
			HighWaterMark:         10,
			RetryingHighWaterMark: 0, // graph-write pressure gate disabled
			PollInterval:          time.Second,
		},
		deferral:         newDeferralState(),
		failureClassSink: recorder.record,
		sleep:            func(context.Context, time.Duration) error { return nil },
	}

	if _, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got, want := recorder.last(), DeferralReasonHighWater; got != want {
		t.Fatalf("deferral reason = %q, want %q", got, want)
	}
}

// TestReducerAdmissionGraphWritePressureConcurrentEnqueueShareState proves the
// hysteresis state is shared and race-free across concurrent producer Enqueue
// calls: both the ingester and bootstrap-index run projection workers
// concurrently and share one admission writer value. Run with -race.
func TestReducerAdmissionGraphWritePressureConcurrentEnqueueShareState(t *testing.T) {
	t.Parallel()

	reader := alwaysLowRetryingDepthReader{}
	inner := &syncCountingIntentWriter{}
	admission := writer{
		inner:       inner,
		depthReader: reader,
		config: Config{
			HighWaterMark:         10_000,
			RetryingHighWaterMark: 25,
			RetryingLowWaterMark:  10,
			PollInterval:          time.Millisecond,
		},
		deferral: newDeferralState(),
		sleep:    func(context.Context, time.Duration) error { return nil },
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
				{Domain: reducer.DomainWorkloadIdentity},
			}); err != nil {
				t.Errorf("Enqueue() error = %v, want nil", err)
			}
		}()
	}
	wg.Wait()
	if got, want := inner.total(), 16; got != want {
		t.Fatalf("enqueued intents = %d, want %d", got, want)
	}
}

// syncCountingIntentWriter is a concurrency-safe inner writer for the
// concurrent admission test. The production producer may call Enqueue from
// multiple projection workers, so only the shared hysteresis state must be
// race-free; this fake simply tallies safely so the test asserts no intent is
// lost.
type syncCountingIntentWriter struct {
	mu    sync.Mutex
	count int
}

func (w *syncCountingIntentWriter) Enqueue(
	_ context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	w.mu.Lock()
	w.count += len(intents)
	w.mu.Unlock()
	return projector.IntentResult{Count: len(intents)}, nil
}

func (w *syncCountingIntentWriter) total() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.count
}

func TestLoadConfigGraphWritePressure(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK": "500",
		"ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK":  "100",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := config.RetryingHighWaterMark, int64(500); got != want {
		t.Fatalf("RetryingHighWaterMark = %d, want %d", got, want)
	}
	if got, want := config.RetryingLowWaterMark, int64(100); got != want {
		t.Fatalf("RetryingLowWaterMark = %d, want %d", got, want)
	}
}

func TestLoadConfigGraphWritePressureDefaultsEnabled(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapGetenv(nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if config.RetryingHighWaterMark != defaultRetryingHighWaterMark {
		t.Fatalf("RetryingHighWaterMark = %d, want default %d",
			config.RetryingHighWaterMark, defaultRetryingHighWaterMark)
	}
	if config.RetryingLowWaterMark != defaultRetryingLowWaterMark {
		t.Fatalf("RetryingLowWaterMark = %d, want default %d",
			config.RetryingLowWaterMark, defaultRetryingLowWaterMark)
	}
	if !config.graphWritePressureEnabled() {
		t.Fatal("default graph-write pressure gate is disabled, want enabled")
	}
}

func TestLoadConfigGraphWritePressureExplicitZeroDisables(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK": "0",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if config.graphWritePressureEnabled() {
		t.Fatal("explicit zero graph-write pressure gate is enabled, want disabled")
	}
}

func TestLoadConfigRejectsLowWaterAboveHighWater(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK": "100",
		"ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK":  "200",
	}))
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want low>=high validation error")
	}
}

func TestLoadConfigRejectsNegativeRetryingMark(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK": "-1",
	}))
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want invalid config error")
	}
}

type recordingDeferralReasonReader struct {
	mu      sync.Mutex
	reasons []string
}

func (r *recordingDeferralReasonReader) record(_ context.Context, reason, _ string, _ int64, _ int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reasons = append(r.reasons, reason)
}

func (r *recordingDeferralReasonReader) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reasons) == 0 {
		return ""
	}
	return r.reasons[len(r.reasons)-1]
}

type alwaysLowRetryingDepthReader struct{}

func (alwaysLowRetryingDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	return map[string]map[string]int64{"reducer": {"pending": 1, "retrying": 1}}, nil
}

func (alwaysLowRetryingDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	return 1, nil
}
