// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crashreplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/clock"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// defaultLeaseTTL is how long a claim holds its lease before a crashed item
// becomes reclaimable. Recovery advances the simulated clock past it, so the
// exact value only needs to be positive and stable.
const defaultLeaseTTL = time.Minute

// simStart anchors the simulated clock. Recovery advances from here; the value
// is fixed so runs are deterministic.
var simStart = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// Config parameterizes one crash-replay run.
type Config struct {
	// Items is the work-item delivery order (e.g. schedulereplay.ScheduleInOrder
	// over cassette-loaded items). IntentIDs must be unique.
	Items []schedulereplay.WorkItem
	// LeaseTTL is the claim lease duration. Zero uses defaultLeaseTTL.
	LeaseTTL time.Duration
	// Apply contributes a work item to the shared graph. Nil uses
	// schedulereplay.ApplyCanonical (the idempotent, order-independent applier).
	Apply schedulereplay.Applier
}

// Report records what a crash-replay run observed across the crash and recovery
// phases, so a scenario can assert recovery behaved correctly rather than only
// comparing snapshots.
type Report struct {
	// Crashed is true when the scripted crash actually fired.
	Crashed bool
	// PreCrashAcks is how many items were durably completed before the crash.
	PreCrashAcks int
	// RecoveryAcks is how many items the recovery phase completed.
	RecoveryAcks int
	// ReclaimedAfterCrash is how many lapsed leases recovery took over — the
	// dirty items the crash left mid-flight.
	ReclaimedAfterCrash int
	// MaxAttempt is the highest fencing token any item reached. >= 2 proves a
	// reclaim happened under an advanced token.
	MaxAttempt int
	// DoubleAcks is how many items were completed more than once. Correct
	// recovery keeps this at 0.
	DoubleAcks int
}

// Outcome is the result of a crash-replay run: the recovered canonical graph
// snapshot plus the run report.
type Outcome struct {
	Snapshot []byte
	Report   Report
}

// RunToCompletion drives the work items through the real reducer service loop
// against the durable store with no crash, and returns the converged canonical
// graph snapshot. It is the baseline a crash run is compared against.
func RunToCompletion(ctx context.Context, cfg Config) ([]byte, error) {
	h, err := newHarness(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := h.drain(ctx, nil); err != nil {
		return nil, err
	}
	if !h.store.drained() {
		return nil, fmt.Errorf("crashreplay: no-crash run did not drain: %d/%d completed",
			h.store.ackedCount(), h.store.total())
	}
	snap, err := h.graph.Canonical()
	if err != nil {
		return nil, fmt.Errorf("crashreplay: canonical snapshot: %w", err)
	}
	return snap, nil
}

// RunWithCrash drives the work items through the reducer loop, injects the
// scripted crash, drops the in-memory worker, advances the clock so the crashed
// item's lease lapses, and replays the remainder from durable state. It returns
// the recovered snapshot and a report. It fails loudly if the crash never fired
// (so a misconfigured crash point cannot masquerade as a green run) or if
// recovery does not drain.
func RunWithCrash(ctx context.Context, cfg Config, crash CrashPoint) (Outcome, error) {
	if err := crash.validate(); err != nil {
		return Outcome{}, err
	}
	h, err := newHarness(cfg)
	if err != nil {
		return Outcome{}, err
	}

	ctrl := &crashController{kind: crash.Kind, after: crash.After}
	crashed, err := h.drain(ctx, ctrl)
	if err != nil {
		return Outcome{}, fmt.Errorf("crashreplay pre-crash phase: %w", err)
	}
	if !crashed {
		return Outcome{}, fmt.Errorf(
			"crashreplay: crash point (kind=%s after=%d) never triggered; the schedule drained without crashing",
			crash.Kind, crash.After,
		)
	}
	preCrashAcks := h.store.ackedCount()

	// Simulate the crash: the in-memory worker is gone. Advance the simulated
	// clock past the lease TTL so any lease the crashed worker still held lapses
	// and the item becomes reclaimable — the durable-state recovery path.
	h.clk.Advance(h.leaseTTL + time.Second)

	if _, err := h.drain(ctx, nil); err != nil {
		return Outcome{}, fmt.Errorf("crashreplay recovery phase: %w", err)
	}
	if !h.store.drained() {
		return Outcome{}, fmt.Errorf("crashreplay: recovery did not drain: %d/%d completed",
			h.store.ackedCount(), h.store.total())
	}

	snap, err := h.graph.Canonical()
	if err != nil {
		return Outcome{}, fmt.Errorf("crashreplay: canonical snapshot: %w", err)
	}
	return Outcome{
		Snapshot: snap,
		Report: Report{
			Crashed:             true,
			PreCrashAcks:        preCrashAcks,
			RecoveryAcks:        h.store.ackedCount() - preCrashAcks,
			ReclaimedAfterCrash: h.store.reclaimCount(),
			MaxAttempt:          h.store.maxAttemptSeen(),
			DoubleAcks:          h.store.doubleAckCount(),
		},
	}, nil
}

// harness holds the durable state that survives a crash (the store, the graph,
// and the simulated clock) plus the applier and registry the executor needs. One
// harness is reused across the crash and recovery phases — that reuse is the
// point: the graph and store are durable, only the reducer service goroutine is
// thrown away between phases.
type harness struct {
	store    *durableStore
	graph    *schedulereplay.Graph
	clk      *clock.Simulated
	apply    schedulereplay.Applier
	registry map[string]schedulereplay.WorkItem
	leaseTTL time.Duration
}

// newHarness builds the durable state for one scenario.
func newHarness(cfg Config) (*harness, error) {
	if len(cfg.Items) == 0 {
		return nil, fmt.Errorf("crashreplay: config has no work items")
	}
	apply := cfg.Apply
	if apply == nil {
		apply = schedulereplay.ApplyCanonical
	}
	leaseTTL := cfg.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = defaultLeaseTTL
	}
	registry := make(map[string]schedulereplay.WorkItem, len(cfg.Items))
	for _, item := range cfg.Items {
		registry[item.IntentID] = item
	}
	clk := clock.NewSimulated(simStart)
	store, err := newDurableStore(cfg.Items, clk, leaseTTL)
	if err != nil {
		return nil, err
	}
	return &harness{
		store:    store,
		graph:    schedulereplay.NewGraph(),
		clk:      clk,
		apply:    apply,
		registry: registry,
		leaseTTL: leaseTTL,
	}, nil
}

// phaseResult is what one reducer phase goroutine reports back.
type phaseResult struct {
	crashed bool
	err     error
}

// drain runs one reducer service phase against the durable store and shared
// graph until the schedule drains or the scripted crash fires. When ctrl is
// non-nil the source and executor are wrapped with crash decorators; when nil
// the phase runs to completion (baseline or recovery). It returns whether the
// crash fired and the first execute or loop error.
func (h *harness) drain(ctx context.Context, ctrl *crashController) (bool, error) {
	exec := &graphExecutor{registry: h.registry, graph: h.graph, apply: h.apply, crash: ctrl}
	var source reducer.WorkSource = h.store
	if ctrl != nil {
		source = &crashingSource{inner: h.store, crash: ctrl}
	}
	svc := reducer.Service{
		PollInterval: time.Millisecond,
		WorkSource:   source,
		Executor:     exec,
		WorkSink:     h.store,
		Workers:      1,
		Wait:         ctxAwareWait,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan phaseResult, 1)
	go func() {
		defer func() {
			switch rec := recover(); rec {
			case nil:
				// svc.Run returned normally; the result was already sent.
			case crashSentinel:
				done <- phaseResult{crashed: true}
			case fatalSentinel:
				// An unrecoverable executor error unwound the loop. Surface the
				// recorded error (firstErr) so the phase fails loudly instead of
				// the reducer Fail+re-queue loop spinning forever.
				err := exec.firstErr()
				if err == nil {
					err = fmt.Errorf("crashreplay: fatal executor stop with no recorded error")
				}
				done <- phaseResult{err: err}
			default:
				panic(rec) // not our sentinel — a real bug, do not swallow it
			}
		}()
		done <- phaseResult{err: svc.Run(runCtx)}
	}()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case res := <-done:
			if execErr := exec.firstErr(); execErr != nil {
				return res.crashed, execErr
			}
			return res.crashed, loopExitError(res.err)
		case <-ctx.Done():
			cancel()
			res := <-done
			if res.crashed {
				return true, nil
			}
			return false, fmt.Errorf("crashreplay phase canceled before drain (acked=%d/%d): %w",
				h.store.ackedCount(), h.store.total(), ctx.Err())
		case <-ticker.C:
			// The reducer loop polls forever, so a phase that is not going to
			// crash never returns on its own; cancel it once the schedule
			// drains. A fired crash leaves the schedule un-drained (items remain
			// incomplete), so this only triggers for a no-crash, recovery, or
			// unreachable-crash phase — exactly the ones that must stop on drain.
			if h.store.drained() {
				cancel()
			}
		}
	}
}

// loopExitError normalizes the reducer loop's return: a context cancellation is
// the expected stop signal once work has drained, not a failure.
func loopExitError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// ctxAwareWait is the reducer poll wait that respects cancellation, so a drained
// phase stops promptly when the runner cancels the context.
func ctxAwareWait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("crashreplay wait canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
