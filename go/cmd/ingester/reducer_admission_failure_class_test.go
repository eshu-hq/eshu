// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerAdmissionReadinessBacklogDoesNotThrottle proves the
// graph-write-pressure gate is scoped to the graph-write-timeout failure class
// and never throttles on reducer readiness backlogs. A large pile of
// readiness-not-ready retrying rows (secrets_iam_endpoint_not_ready and other
// *_n classes) reports zero graph-write-timeout depth, so the producer admits
// source-local reducer intent immediately even though total retrying depth is
// far above the graph-write high-water mark. This is the #3560 P2 false-throttle
// fix: readiness backlogs must not pause unrelated reducer admission.
func TestReducerAdmissionReadinessBacklogDoesNotThrottle(t *testing.T) {
	t.Parallel()

	reader := &classScopedDepthReader{
		// 800 retrying rows are all readiness-not-ready; zero are
		// graph-write timeouts. The graph backend is perfectly healthy.
		depths:                 []map[string]map[string]int64{{"reducer": {"pending": 5, "retrying": 800}}},
		graphWriteTimeoutDepth: []int64{0},
	}
	writer := &recordingReducerIntentWriter{}
	sleeps := 0
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark:         10_000,
			RetryingHighWaterMark: 500,
			RetryingLowWaterMark:  100,
			PollInterval:          time.Second,
		},
		deferral: newAdmissionDeferralState(),
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
	if sleeps != 0 {
		t.Fatalf("sleep count = %d, want 0 (readiness backlog must not throttle)", sleeps)
	}
	if writer.calls != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", writer.calls)
	}
}

// TestReducerAdmissionGraphWriteTimeoutBacklogThrottles proves the gate still
// engages when the retrying backlog is genuine graph-write-timeout pressure.
// Here the graph-write-timeout depth alone exceeds the high-water mark, so the
// producer defers until it recovers below the low-water mark, even though total
// retrying depth is identical to the readiness-only case above.
func TestReducerAdmissionGraphWriteTimeoutBacklogThrottles(t *testing.T) {
	t.Parallel()

	reader := &classScopedDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"pending": 5, "retrying": 800}},
			{"reducer": {"pending": 5, "retrying": 50}},
		},
		// First read: 600 graph-write timeouts (above high-water 500) -> defer.
		// Second read: 40 graph-write timeouts (below low-water 100) -> resume.
		graphWriteTimeoutDepth: []int64{600, 40},
	}
	writer := &recordingReducerIntentWriter{}
	sleeps := 0
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark:         10_000,
			RetryingHighWaterMark: 500,
			RetryingLowWaterMark:  100,
			PollInterval:          time.Second,
		},
		deferral: newAdmissionDeferralState(),
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
	if sleeps != 1 {
		t.Fatalf("sleep count = %d, want 1 (graph-write-timeout backlog must throttle)", sleeps)
	}
	if writer.calls != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", writer.calls)
	}
}

// TestReducerAdmissionGraphWritePressureRecordsFailureClass proves the deferral
// telemetry names the failure class that drove the pressure signal, so an
// operator can confirm at 3 AM that graph-write timeouts (not readiness
// backlogs) caused the producer to back off.
func TestReducerAdmissionGraphWritePressureRecordsFailureClass(t *testing.T) {
	t.Parallel()

	reader := &classScopedDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"retrying": 600}},
			{"reducer": {"retrying": 10}},
		},
		graphWriteTimeoutDepth: []int64{600, 10},
	}
	recorder := &recordingFailureClassReader{}
	admission := reducerAdmissionWriter{
		inner:       &recordingReducerIntentWriter{},
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark:         10_000,
			RetryingHighWaterMark: 500,
			RetryingLowWaterMark:  100,
			PollInterval:          time.Second,
		},
		deferral:         newAdmissionDeferralState(),
		failureClassSink: recorder.record,
		sleep:            func(context.Context, time.Duration) error { return nil },
	}

	if _, err := admission.Enqueue(context.Background(), []projector.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got, want := recorder.last(), admissionGraphWriteTimeoutFailureClass; got != want {
		t.Fatalf("deferral failure class = %q, want %q", got, want)
	}
}

// classScopedDepthReader is a depth reader whose graph-write-timeout retrying
// depth is reported independently of the total retrying bucket. It lets the
// failure-class tests prove the gate counts only graph-write-timeout rows.
type classScopedDepthReader struct {
	depths                 []map[string]map[string]int64
	graphWriteTimeoutDepth []int64
	depthCalls             int
	timeoutCalls           int
}

func (r *classScopedDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	if r.depthCalls >= len(r.depths) {
		return r.depths[len(r.depths)-1], nil
	}
	depth := r.depths[r.depthCalls]
	r.depthCalls++
	return depth, nil
}

func (r *classScopedDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	if r.timeoutCalls >= len(r.graphWriteTimeoutDepth) {
		return r.graphWriteTimeoutDepth[len(r.graphWriteTimeoutDepth)-1], nil
	}
	depth := r.graphWriteTimeoutDepth[r.timeoutCalls]
	r.timeoutCalls++
	return depth, nil
}

type recordingFailureClassReader struct {
	classes []string
}

func (r *recordingFailureClassReader) record(_ context.Context, _, failureClass string, _ int64, _ int) {
	r.classes = append(r.classes, failureClass)
}

func (r *recordingFailureClassReader) last() string {
	if len(r.classes) == 0 {
		return ""
	}
	return r.classes[len(r.classes)-1]
}
