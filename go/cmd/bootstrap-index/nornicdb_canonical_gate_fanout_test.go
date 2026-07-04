// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// concurrencyProbeGroupExecutor is a GroupExecutor that records the peak
// number of concurrent ExecuteGroup calls it sees and blocks until released,
// so a test can pile up many callers and observe how many run at once.
type concurrencyProbeGroupExecutor struct {
	current     int64
	peak        int64
	callCount   int64
	release     chan struct{}
	releaseOnce sync.Once
}

func (p *concurrencyProbeGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (p *concurrencyProbeGroupExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	atomic.AddInt64(&p.callCount, 1)
	cur := atomic.AddInt64(&p.current, 1)
	defer atomic.AddInt64(&p.current, -1)
	for {
		peak := atomic.LoadInt64(&p.peak)
		if cur <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&p.peak, peak, cur) {
			break
		}
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (p *concurrencyProbeGroupExecutor) peakConcurrency() int64 {
	return atomic.LoadInt64(&p.peak)
}

func (p *concurrencyProbeGroupExecutor) calls() int64 {
	return atomic.LoadInt64(&p.callCount)
}

func (p *concurrencyProbeGroupExecutor) unblock() {
	p.releaseOnce.Do(func() { close(p.release) })
}

// TestBootstrapCanonicalGateBoundsConcurrentEntityFanOut is the confirmed-bug
// regression from the PR #4662 review: the backpressure gate must wrap the
// GroupExecutor layer INSIDE bootstrapNornicDBPhaseGroupExecutor, not the
// outermost PhaseGroupExecutor entry point, because
// executeEntityPhaseGroupConcurrently fans a single ExecutePhaseGroup call out
// into up to entityPhaseConcurrency concurrent ge.ExecuteGroup calls. Gating
// only the outer ExecutePhaseGroup call (one permit per call) would let all
// entityPhaseConcurrency inner ExecuteGroup calls run at once regardless of
// the configured ceiling.
//
// This test drives ExecutePhaseGroup with entityPhaseConcurrency (8) greater
// than the gate's MaxInFlight (2) ceiling and asserts peak concurrent
// ExecuteGroup calls against the probe never exceeds 2. Before the fix (gate
// wrapped outermost around ExecutePhaseGroup), this would observe a peak of up
// to 8; after the fix (gate wrapped around the inner GroupExecutor layer),
// peak must be <= 2.
func TestBootstrapCanonicalGateBoundsConcurrentEntityFanOut(t *testing.T) {
	t.Parallel()

	const (
		entityPhaseConcurrency = 8
		maxInFlight            = 2
		chunkCount             = 16
	)

	probe := &concurrencyProbeGroupExecutor{release: make(chan struct{})}
	gate := graphbackpressure.NewGate(maxInFlight, nil, graphbackpressure.CanonicalGateName)

	executor, err := bootstrapCanonicalExecutorForGraphBackend(
		probe,
		runtimecfg.GraphBackendNornicDB,
		func(key string) string {
			switch key {
			case nornicDBEntityPhaseConcurrencyEnv:
				return "8"
			case nornicDBEntityPhaseStatementsEnv:
				// One statement per chunk so entityPhaseConcurrency chunks are
				// produced from chunkCount statements sharing one label.
				return "1"
			default:
				return ""
			}
		},
		nil,
		nil,
		gate,
	)
	if err != nil {
		t.Fatalf("bootstrapCanonicalExecutorForGraphBackend() error = %v, want nil", err)
	}
	phaseExecutor, ok := executor.(bootstrapNornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want bootstrapNornicDBPhaseGroupExecutor", executor)
	}

	stmts := make([]sourcecypher.Statement, chunkCount)
	for i := range stmts {
		stmts[i] = bootstrapEntityPhaseStatement()
	}

	done := make(chan error, 1)
	go func() {
		done <- phaseExecutor.ExecutePhaseGroup(context.Background(), stmts)
	}()

	// Wait deterministically until the probe reports it has REACHED the
	// gate's ceiling (proof the gate is actually engaged AND actually allows
	// maxInFlight concurrent writes, not fewer), rather than sleeping a fixed
	// duration and hoping. Every chunk blocks on probe.release until we
	// unblock it below, so once peak reaches maxInFlight it cannot un-reach
	// it before we sample the final peak.
	deadline := time.Now().Add(5 * time.Second)
	for probe.peakConcurrency() < int64(maxInFlight) {
		if time.Now().After(deadline) {
			probe.unblock()
			<-done
			t.Fatalf("peak concurrency = %d, never reached ceiling %d within deadline "+
				"(gate is over-serializing the fan-out, a \"Serialization Is Not A Fix\" regression)",
				probe.peakConcurrency(), maxInFlight)
		}
		time.Sleep(5 * time.Millisecond)
	}
	probe.unblock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ExecutePhaseGroup did not complete (permit leak / deadlock)")
	}

	// Peak must be exactly maxInFlight: the wait loop above already proved
	// peak reached the ceiling, so peak > maxInFlight here would mean the
	// gate let more than maxInFlight writes run concurrently (bound
	// breached), which is the confirmed-bug regression this test guards
	// against (gating only the outer ExecutePhaseGroup call would let all
	// entityPhaseConcurrency=8 inner ExecuteGroup calls run at once).
	if peak := probe.peakConcurrency(); peak != maxInFlight {
		t.Fatalf("peak concurrent ExecuteGroup calls = %d, want exactly %d (gate must bound the inner fan-out to exactly the ceiling, not just the outer ExecutePhaseGroup call)", peak, maxInFlight)
	}
	if got, want := probe.calls(), int64(chunkCount); got != want {
		t.Fatalf("ExecuteGroup call count = %d, want %d (every chunk must still execute)", got, want)
	}
}

// TestBootstrapCanonicalGateTerminatesUnderMixedFanOutPressure proves
// deadlock-freedom: concurrent inner ExecuteGroup calls driven through the
// entity-phase fan-out, some succeeding and some failing, must all complete
// and release their permits so the whole ExecutePhaseGroup call terminates
// within a bounded deadline.
func TestBootstrapCanonicalGateTerminatesUnderMixedFanOutPressure(t *testing.T) {
	t.Parallel()

	const (
		entityPhaseConcurrency = 6
		maxInFlight            = 3
		chunkCount             = 12
	)

	probe := &mixedResultGroupExecutor{failEvery: 3}
	gate := graphbackpressure.NewGate(maxInFlight, nil, graphbackpressure.CanonicalGateName)

	executor, err := bootstrapCanonicalExecutorForGraphBackend(
		probe,
		runtimecfg.GraphBackendNornicDB,
		func(key string) string {
			switch key {
			case nornicDBEntityPhaseConcurrencyEnv:
				return "6"
			case nornicDBEntityPhaseStatementsEnv:
				return "1"
			default:
				return ""
			}
		},
		nil,
		nil,
		gate,
	)
	if err != nil {
		t.Fatalf("bootstrapCanonicalExecutorForGraphBackend() error = %v, want nil", err)
	}
	phaseExecutor, ok := executor.(bootstrapNornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want bootstrapNornicDBPhaseGroupExecutor", executor)
	}

	stmts := make([]sourcecypher.Statement, chunkCount)
	for i := range stmts {
		stmts[i] = bootstrapEntityPhaseStatement()
	}

	done := make(chan error, 1)
	go func() {
		done <- phaseExecutor.ExecutePhaseGroup(context.Background(), stmts)
	}()

	select {
	case <-done:
		// Some chunks fail by design (failEvery), so ExecutePhaseGroup
		// returning an error is expected; the point of this test is that it
		// returns at all within the deadline, proving no permit leak.
	case <-time.After(10 * time.Second):
		t.Fatal("ExecutePhaseGroup did not terminate under mixed success/failure pressure (permit leak / deadlock)")
	}

	// Peak must reach exactly maxInFlight: with 12 chunks, 6-way fan-out, and
	// a 20ms hold per call, 6 concurrent callers reliably contend for the 3
	// permits, so peak staying below 3 would mean the gate over-serialized
	// the fan-out instead of allowing the configured concurrency budget (the
	// same "Serialization Is Not A Fix" failure mode a peak<=maxInFlight-only
	// assertion would miss), while peak above 3 would mean the bound itself
	// was breached under mixed success/failure pressure.
	if peak := probe.peakConcurrency(); peak != maxInFlight {
		t.Fatalf("peak concurrent ExecuteGroup calls = %d, want exactly %d", peak, maxInFlight)
	}
}

// mixedResultGroupExecutor fails every Nth ExecuteGroup call immediately
// (no blocking) so a mixed success/failure fan-out can be driven without a
// release channel, while still tracking peak concurrency.
type mixedResultGroupExecutor struct {
	current   int64
	peak      int64
	callCount int64
	failEvery int64
}

func (m *mixedResultGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (m *mixedResultGroupExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	n := atomic.AddInt64(&m.callCount, 1)
	cur := atomic.AddInt64(&m.current, 1)
	defer atomic.AddInt64(&m.current, -1)
	for {
		peak := atomic.LoadInt64(&m.peak)
		if cur <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&m.peak, peak, cur) {
			break
		}
	}
	// Small sleep so concurrent callers overlap even without a release gate.
	time.Sleep(20 * time.Millisecond)
	if m.failEvery > 0 && n%m.failEvery == 0 {
		return sourcecypher.GraphWriteTimeoutError{Operation: "test", Timeout: time.Second}
	}
	return nil
}

func (m *mixedResultGroupExecutor) peakConcurrency() int64 {
	return atomic.LoadInt64(&m.peak)
}

// TestBootstrapCanonicalGateDisabledFanOutIsUnbounded proves the passthrough
// contract at the fan-out layer: a nil gate (default, unset ceiling) leaves
// the entity-phase concurrent fan-out able to reach its full configured
// entityPhaseConcurrency, matching pre-existing behavior with the gate
// disabled.
func TestBootstrapCanonicalGateDisabledFanOutIsUnbounded(t *testing.T) {
	t.Parallel()

	const (
		entityPhaseConcurrency = 4
		chunkCount             = 8
	)

	probe := &concurrencyProbeGroupExecutor{release: make(chan struct{})}

	executor, err := bootstrapCanonicalExecutorForGraphBackend(
		probe,
		runtimecfg.GraphBackendNornicDB,
		func(key string) string {
			switch key {
			case nornicDBEntityPhaseConcurrencyEnv:
				return "4"
			case nornicDBEntityPhaseStatementsEnv:
				return "1"
			default:
				return ""
			}
		},
		nil,
		nil,
		nil, // disabled gate
	)
	if err != nil {
		t.Fatalf("bootstrapCanonicalExecutorForGraphBackend() error = %v, want nil", err)
	}
	phaseExecutor, ok := executor.(bootstrapNornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want bootstrapNornicDBPhaseGroupExecutor", executor)
	}

	stmts := make([]sourcecypher.Statement, chunkCount)
	for i := range stmts {
		stmts[i] = bootstrapEntityPhaseStatement()
	}

	done := make(chan error, 1)
	go func() {
		done <- phaseExecutor.ExecutePhaseGroup(context.Background(), stmts)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for probe.peakConcurrency() < int64(entityPhaseConcurrency) {
		if time.Now().After(deadline) {
			probe.unblock()
			<-done
			t.Fatalf("peak concurrency = %d, want to reach %d with the gate disabled",
				probe.peakConcurrency(), entityPhaseConcurrency)
		}
		time.Sleep(5 * time.Millisecond)
	}
	probe.unblock()

	if err := <-done; err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
}
