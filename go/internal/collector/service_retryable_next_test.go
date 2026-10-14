// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// skipError is a retryable source failure that opts into the cycle-skip path.
type skipError struct {
	skip CycleSkip
	err  error
}

func (e *skipError) Error() string        { return fmt.Sprintf("skippable: %v", e.err) }
func (e *skipError) Unwrap() error        { return e.err }
func (e *skipError) Retryable() bool      { return true }
func (e *skipError) CycleSkip() CycleSkip { return e.skip }
func newSkipError(retryAfter time.Duration, consecutive int) *skipError {
	return &skipError{
		skip: CycleSkip{
			CollectorKind: "git", FailureClass: "server_error", Operation: "mint",
			Attempts: 4, Consecutive: consecutive, RetryAfter: retryAfter,
		},
		err: errors.New("list GitHub org repositories: status 504"),
	}
}

// retryableOnly is retryable but does not describe a cycle skip, so it keeps
// the fatal path.
type retryableOnly struct{}

func (retryableOnly) Error() string   { return "retryable without a skip description" }
func (retryableOnly) Retryable() bool { return true }

// scriptedSource answers Next from a script; after the script it cancels the
// run and reports the context error, so a test ends by shutdown.
type scriptedSource struct {
	mu     sync.Mutex
	steps  []scriptedStep
	calls  int
	cancel context.CancelFunc
}

type scriptedStep struct {
	collected CollectedGeneration
	ok        bool
	err       error
}

func (s *scriptedSource) Next(ctx context.Context) (CollectedGeneration, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.steps) {
		s.cancel()
		return CollectedGeneration{}, false, ctx.Err()
	}
	step := s.steps[s.calls]
	s.calls++
	return step.collected, step.ok, step.err
}

// waitRecorder is an injectable Service.Wait that records each wait and
// returns the scripted error, if any, on the n-th call.
type waitRecorder struct {
	mu      sync.Mutex
	waits   []time.Duration
	failOn  int
	failErr error
}

func (w *waitRecorder) wait(_ context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.waits = append(w.waits, d)
	if w.failOn > 0 && len(w.waits) == w.failOn {
		return w.failErr
	}
	return nil
}

func (w *waitRecorder) recorded() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.waits...)
}

// TestServiceRunSkipsRetryableNextErrors pins D1: a retryable Next error that
// describes a cycle skip waits RetryAfter (PollInterval when zero), spends no
// drain, and the loop keeps going. The drain hook and the escape stay unused:
// a failed listing is not a drained batch.
func TestServiceRunSkipsRetryableNextErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &scriptedSource{
		steps: []scriptedStep{
			{err: fmt.Errorf("source next: %w", newSkipError(7*time.Second, 1))},
			{err: newSkipError(0, 2)},
			{err: newSkipError(90*time.Second, 3)},
		},
		cancel: cancel,
	}
	waits := &waitRecorder{}
	service := Service{
		Source:                 source,
		Committer:              &stubCommitter{},
		PollInterval:           3 * time.Second,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(context.Context, bool) error {
			t.Error("AfterBatchDrained called for a failed discovery: the escape latch was spent")
			return nil
		},
		Wait: waits.wait,
	}
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil on shutdown", err)
	}
	want := []time.Duration{7 * time.Second, 3 * time.Second, 90 * time.Second}
	if got := waits.recorded(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	if source.calls != 3 {
		t.Fatalf("Next calls = %d, want 3 scripted calls before shutdown", source.calls)
	}
}

// TestServiceRunStillDrainsAfterASkippedCycleThenACommit pins that skipped
// cycles do not disturb the normal drain bookkeeping once work commits.
func TestServiceRunStillDrainsAfterASkippedCycleThenACommit(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &scriptedSource{
		steps: []scriptedStep{
			{err: newSkipError(time.Second, 1)},
			{collected: FactsFromSlice(scopeValue, generationValue, envelopes), ok: true},
			{}, // idle poll: the batch is drained
		},
		cancel: cancel,
	}
	var drains []bool
	service := Service{
		Source: source,
		Committer: &stubCommitter{commit: func(_ context.Context, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope) error {
			for range stream {
			}
			return nil
		}},
		PollInterval:           time.Second,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(_ context.Context, hasCommitted bool) error {
			drains = append(drains, hasCommitted)
			return nil
		},
		Wait: (&waitRecorder{}).wait,
	}
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(drains) != 1 || !drains[0] {
		t.Fatalf("drains = %v, want exactly one drain reporting a real commit", drains)
	}
}

// TestServiceRunKeepsFatalPathsFatal pins D1's refusals: a plain error is
// fatal, and a retryable error that does not opt into the skip path is fatal,
// so a permanent fault still ends the process.
func TestServiceRunKeepsFatalPathsFatal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"plain", errors.New("list GitHub org repositories: status 401")},
		{"retryable without a skip description", retryableOnly{}},
		// The second IsRetryable branch: an owner's cap error that wraps a
		// retryable registry failure with %w must not become a skip loop.
		{"registry retryable failure wrapped at a cap", fmt.Errorf(
			"OCI registry scan failed 20 consecutive scan cycles: %w",
			RegistryFailure{Class: RegistryFailureRetryable, Message: "registry collector failed"},
		)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			waits := &waitRecorder{}
			service := Service{
				Source:       &scriptedSource{steps: []scriptedStep{{err: tc.err}}, cancel: cancel},
				Committer:    &stubCommitter{},
				PollInterval: time.Second,
				Wait:         waits.wait,
			}
			err := service.Run(ctx)
			if err == nil || !strings.HasPrefix(err.Error(), "collect scope generation: ") {
				t.Fatalf("Run() error = %v, want collect scope generation: ...", err)
			}
			if got := waits.recorded(); len(got) != 0 {
				t.Fatalf("waits = %v, want none", got)
			}
		})
	}
}

// TestServiceRunReturnsNilWhenCancelledDuringTheSkipWait pins shutdown during
// a skipped-cycle wait: the same nil return as a shutdown anywhere else.
func TestServiceRunReturnsNilWhenCancelledDuringTheSkipWait(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := &waitRecorder{failOn: 1, failErr: context.Canceled}
	service := Service{
		Source:       &scriptedSource{steps: []scriptedStep{{err: newSkipError(time.Minute, 1)}}, cancel: cancel},
		Committer:    &stubCommitter{},
		PollInterval: time.Second,
		Wait:         waits.wait,
	}
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil on shutdown during the wait", err)
	}
	if got := waits.recorded(); len(got) != 1 || got[0] != time.Minute {
		t.Fatalf("waits = %v, want one wait of 1m", got)
	}
}

// TestServiceRunSkipSignalsReachTheOperator pins the log line and the counter
// of D1: WARN "collector discovery retryable; skipping cycle" with attempt,
// failure_class, consecutive_failures, retry_after_seconds, retryable=true,
// and eshu_dp_collector_discovery_skipped_cycles_total.
func TestServiceRunSkipSignalsReachTheOperator(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("skip-test"),
	)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := Service{
		Source: &scriptedSource{
			steps:  []scriptedStep{{err: newSkipError(7*time.Second, 1)}, {err: newSkipError(7*time.Second, 2)}},
			cancel: cancel,
		},
		Committer:    &stubCommitter{},
		PollInterval: time.Second,
		Instruments:  instruments,
		Logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
		Wait:         (&waitRecorder{}).wait,
	}
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, fragment := range []string{
		`"msg":"collector discovery retryable; skipping cycle"`,
		`"level":"WARN"`,
		`"failure_class":"server_error"`,
		`"operation":"mint"`,
		`"consecutive_failures":2`,
		`"attempt":4`,
		`"retry_after_seconds":7`,
		`"retryable":true`,
	} {
		if !strings.Contains(logs.String(), fragment) {
			t.Fatalf("skip log %q missing %s", logs.String(), fragment)
		}
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var got int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_collector_discovery_skipped_cycles_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				attrs := dp.Attributes.ToSlice()
				var kind, class string
				for _, a := range attrs {
					switch string(a.Key) {
					case "collector_kind":
						kind = a.Value.AsString()
					case "failure_class":
						class = a.Value.AsString()
					}
				}
				if kind == "git" && class == "server_error" {
					got += dp.Value
				}
			}
		}
	}
	if got != 2 {
		t.Fatalf("skipped cycles = %d, want 2", got)
	}
}
