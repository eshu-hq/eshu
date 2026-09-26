// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// testClaimRetry keeps the conflict cap small and the wait tiny so the cap and
// reset tests run in milliseconds instead of maxConsecutiveClaimConflicts * 500ms.
func testClaimRetry(maxConflicts int) claimRetry {
	return claimRetry{wait: time.Millisecond, maxConsecutiveConflicts: maxConflicts}
}

// runClaimWithGuard runs claimProjectorWorkWith and fails the test instead of
// hanging when the retry loop never ends (the pre-#7122-cap behavior).
func runClaimWithGuard(
	t *testing.T,
	source *conflictThenWorkSource,
	retry claimRetry,
	instruments *telemetry.Instruments,
) (bool, error) {
	t.Helper()
	type result struct {
		ok  bool
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, ok, err := claimProjectorWorkWith(context.Background(), source, 2, nil, instruments, retry)
		done <- result{ok: ok, err: err}
	}()
	select {
	case r := <-done:
		return r.ok, r.err
	case <-time.After(5 * time.Second):
		t.Fatalf("claimProjectorWorkWith still running after 5s and %d Claim calls: a persistent conflict must fail the run, not hang it",
			source.claimCalls())
		return false, nil
	}
}

func TestClaimProjectorWorkFailsAfterConsecutiveConflictCap(t *testing.T) {
	t.Parallel()

	const capN = 4
	source := &conflictThenWorkSource{conflicts: 1 << 30}

	ok, err := runClaimWithGuard(t, source, testClaimRetry(capN), nil)
	if ok {
		t.Fatal("ok = true, want false on a fatal claim error")
	}
	if err == nil {
		t.Fatal("error = nil, want a fatal error after the consecutive-conflict cap")
	}
	if !errors.Is(err, errClaimConflictsExhausted) {
		t.Fatalf("error = %v, want errClaimConflictsExhausted", err)
	}
	if !errors.Is(err, failure.ErrWorkClaimConflict) {
		t.Fatalf("error = %v, want it to wrap the last failure.ErrWorkClaimConflict", err)
	}
	if want := fmt.Sprintf("%d consecutive", capN); !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to name the count (%q)", err, want)
	}
	if got := source.claimCalls(); got != capN {
		t.Fatalf("Claim calls = %d, want exactly %d before failing", got, capN)
	}
}

func TestClaimProjectorWorkConflictCounterResetsOnSuccess(t *testing.T) {
	t.Parallel()

	// capN-1 conflicts stay under the cap; the claim after them succeeds.
	// Repeating the sequence through fresh calls must never accumulate.
	const capN = 4
	source := &conflictThenWorkSource{conflicts: capN - 1, items: conflictTestItems(1)}

	ok, err := runClaimWithGuard(t, source, testClaimRetry(capN), nil)
	if err != nil || !ok {
		t.Fatalf("claim = (ok %v, err %v), want a successful claim after %d conflicts", ok, err, capN-1)
	}
	if got := source.claimCalls(); got != capN {
		t.Fatalf("Claim calls = %d, want %d (conflicts, then work)", got, capN)
	}
}

func TestClaimProjectorWorkConflictCounterResetsOnDrained(t *testing.T) {
	t.Parallel()

	// A drained (empty, no error) result ends the call cleanly; the next call
	// starts a fresh budget instead of inheriting earlier conflicts.
	const capN = 3
	source := &conflictThenWorkSource{conflicts: capN - 1}
	ok, err := runClaimWithGuard(t, source, testClaimRetry(capN), nil)
	if err != nil || ok {
		t.Fatalf("first claim = (ok %v, err %v), want a drained result", ok, err)
	}
	// Force another burst of capN-1 conflicts on the same source.
	source.mu.Lock()
	source.conflicts = source.calls + capN - 1
	source.mu.Unlock()
	ok, err = runClaimWithGuard(t, source, testClaimRetry(capN), nil)
	if err != nil || ok {
		t.Fatalf("second claim = (ok %v, err %v), want a drained result: conflicts must not accumulate across calls", ok, err)
	}
}

func TestClaimProjectorWorkCapErrorIsFatalToDrainProjector(t *testing.T) {
	if testing.Short() {
		t.Skip("waits maxConsecutiveClaimConflicts * claimConflictWait")
	}
	t.Parallel()

	source := &conflictThenWorkSource{conflicts: 1 << 30}
	done := make(chan error, 1)
	go func() {
		done <- drainProjector(
			context.Background(), source, &fakeFactStore{}, &fakeProjectionRunner{}, &concurrentWorkSink{},
			nil, 0, 1, nil, nil, nil,
		)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errClaimConflictsExhausted) {
			t.Fatalf("drainProjector() error = %v, want errClaimConflictsExhausted", err)
		}
		if got := source.claimCalls(); got != maxConsecutiveClaimConflicts {
			t.Fatalf("Claim calls = %d, want %d", got, maxConsecutiveClaimConflicts)
		}
	case <-time.After(time.Duration(maxConsecutiveClaimConflicts)*claimConflictWait + 10*time.Second):
		t.Fatal("drainProjector did not fail after the consecutive-conflict cap")
	}
}

// TestClaimProjectorWorkClaimDurationExcludesConflictWait proves the claim
// duration histogram is recorded per Claim call, so the retry wait between
// conflicting calls does not inflate eshu_dp_queue_claim_duration_seconds.
func TestClaimProjectorWorkClaimDurationExcludesConflictWait(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("claim-duration-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	const wait = 200 * time.Millisecond
	source := &conflictThenWorkSource{conflicts: 2, items: conflictTestItems(1)}

	_, ok, err := claimProjectorWorkWith(context.Background(), source, 0, nil, instruments,
		claimRetry{wait: wait, maxConsecutiveConflicts: 10})
	if err != nil || !ok {
		t.Fatalf("claim = (ok %v, err %v), want success after two conflicts", ok, err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	count, sum := claimDurationHistogram(t, rm)
	if count != 3 {
		t.Fatalf("claim duration samples = %d, want 3 (one per Claim call)", count)
	}
	if sum >= wait.Seconds()/2 {
		t.Fatalf("claim duration sum = %.4fs, want well under one conflict wait (%s): the wait must not be recorded",
			sum, wait)
	}
}

func claimDurationHistogram(t *testing.T, rm metricdata.ResourceMetrics) (uint64, float64) {
	t.Helper()
	const name = "eshu_dp_queue_claim_duration_seconds"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Histogram[float64]", name, m.Data)
			}
			var count uint64
			var sum float64
			for _, p := range hist.DataPoints {
				if metricPointHasAttrs(p.Attributes, map[string]string{"queue": "projector"}) {
					count += p.Count
					sum += p.Sum
				}
			}
			return count, sum
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0, 0
}

// TestDrainProjectorPipelinedSurvivesClaimConflictAfterCollectorDone drives a
// claim conflict through drainProjectorPipelined. With the collector already
// finished, a conflict must neither count as an empty poll (which would end
// the drain early and strand the item behind it) nor end the run: the worker
// waits, claims again, projects the item, then exits only after
// maxEmptyPolls consecutive empty claims.
func TestDrainProjectorPipelinedSurvivesClaimConflictAfterCollectorDone(t *testing.T) {
	t.Parallel()

	source := &conflictThenWorkSource{conflicts: 2, items: conflictTestItems(1)}
	sink := &concurrentWorkSink{}
	collectorDone := make(chan struct{})
	close(collectorDone)
	pd := projectorDeps{
		workSource: source,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	if err := drainProjectorPipelined(context.Background(), pd, 1, collectorDone, nil, nil, nil); err != nil {
		t.Fatalf("drainProjectorPipelined() error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 1 {
		t.Fatalf("acked = %d, want 1: the item behind the conflicts must be projected", got)
	}
	// 2 conflicts + 1 item + 5 consecutive empty polls (maxEmptyPolls). A
	// conflict counted as an empty poll would shorten this.
	if got := source.claimCalls(); got != 8 {
		t.Fatalf("Claim calls = %d, want 8 (2 conflicts, 1 item, 5 empty polls)", got)
	}
}
