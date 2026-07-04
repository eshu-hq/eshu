// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestReducerAdmissionDefersAtHighWaterAndResumesBelow(t *testing.T) {
	t.Parallel()

	reader := &fakeDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"pending": 7, "retrying": 1, "in_flight": 2}},
			{"reducer": {"pending": 10}},
			{"reducer": {"pending": 4}},
		},
	}
	inner := &recordingIntentWriter{}
	sleeps := 0
	admission := writer{
		inner:       inner,
		depthReader: reader,
		config: Config{
			HighWaterMark: 10,
			PollInterval:  time.Second,
		},
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
	if got, want := sleeps, 2; got != want {
		t.Fatalf("sleep count = %d, want %d", got, want)
	}
	if got, want := reader.calls, 3; got != want {
		t.Fatalf("depth read count = %d, want %d", got, want)
	}
	if got, want := inner.calls, 1; got != want {
		t.Fatalf("inner enqueue count = %d, want %d", got, want)
	}
}

func TestReducerAdmissionDefaultConfigDefersAtDefaultHighWaterAndResumesBelow(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapGetenv(nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	reader := &fakeDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"pending": defaultHighWaterMark}},
			{"reducer": {"pending": defaultHighWaterMark - 1}},
		},
	}
	inner := &recordingIntentWriter{}
	sleeps := 0
	admission := writer{
		inner:       inner,
		depthReader: reader,
		config:      config,
		sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	}

	result, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if result.Count != 1 {
		t.Fatalf("Enqueue() count = %d, want 1", result.Count)
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

func TestReducerAdmissionContextCancellationStopsBeforeEnqueue(t *testing.T) {
	t.Parallel()

	expectedErr := context.Canceled
	reader := &fakeDepthReader{
		depths: []map[string]map[string]int64{{"reducer": {"pending": 10}}},
	}
	inner := &recordingIntentWriter{}
	admission := writer{
		inner:       inner,
		depthReader: reader,
		config: Config{
			HighWaterMark: 10,
			PollInterval:  time.Second,
		},
		sleep: func(context.Context, time.Duration) error {
			return expectedErr
		},
	}

	_, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Enqueue() error = %v, want %v", err, expectedErr)
	}
	if inner.calls != 0 {
		t.Fatalf("inner enqueue count = %d, want 0", inner.calls)
	}
}

func TestReducerAdmissionDisabledSkipsDepthRead(t *testing.T) {
	t.Parallel()

	reader := &fakeDepthReader{
		depths: []map[string]map[string]int64{{"reducer": {"pending": 100}}},
	}
	inner := &recordingIntentWriter{}
	admission := writer{
		inner:       inner,
		depthReader: reader,
		config:      Config{},
		sleep: func(context.Context, time.Duration) error {
			t.Fatal("sleep should not run when admission is disabled")
			return nil
		},
	}

	if _, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if reader.calls != 0 {
		t.Fatalf("depth read count = %d, want 0", reader.calls)
	}
	if inner.calls != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", inner.calls)
	}
}

type fakeDepthReader struct {
	depths []map[string]map[string]int64
	calls  int
}

func (f *fakeDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	if f.calls >= len(f.depths) {
		return f.depths[len(f.depths)-1], nil
	}
	depth := f.depths[f.calls]
	f.calls++
	return depth, nil
}

// ReducerGraphWriteTimeoutDepth maps the next depth map's retrying bucket to
// the graph-write-timeout depth so the high-water-only tests (graph-write
// pressure disabled, this method unused) and any pressure test that drives the
// retrying bucket both observe consistent per-iteration depth. The gate calls
// exactly one of QueueDepths / ReducerGraphWriteTimeoutDepth per loop
// iteration, so the shared call cursor advances once per iteration either way.
func (f *fakeDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	if f.calls >= len(f.depths) {
		return f.depths[len(f.depths)-1]["reducer"]["retrying"], nil
	}
	depth := f.depths[f.calls]["reducer"]["retrying"]
	f.calls++
	return depth, nil
}

type recordingIntentWriter struct {
	calls   int
	intents []projector.ReducerIntent
}

func (w *recordingIntentWriter) Enqueue(
	_ context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	w.calls++
	w.intents = append(w.intents, intents...)
	return projector.IntentResult{Count: len(intents)}, nil
}

type countingIntentWriter struct {
	count int
}

func (w *countingIntentWriter) Enqueue(
	_ context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	w.count += len(intents)
	return projector.IntentResult{Count: len(intents)}, nil
}

type fixedDepthReader struct {
	depth map[string]map[string]int64
}

func (f fixedDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	return f.depth, nil
}

func (f fixedDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	return f.depth["reducer"]["retrying"], nil
}

type alternatingDepthReader struct {
	calls int
	high  map[string]map[string]int64
	low   map[string]map[string]int64
}

func (f *alternatingDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	f.calls++
	if f.calls%2 == 1 {
		return f.high, nil
	}
	return f.low, nil
}

func (f *alternatingDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	f.calls++
	if f.calls%2 == 1 {
		return f.high["reducer"]["retrying"], nil
	}
	return f.low["reducer"]["retrying"], nil
}
