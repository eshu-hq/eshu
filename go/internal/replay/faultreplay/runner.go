// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// Config parameterizes one hermetic fault-injection run. It mirrors
// schedulereplay.Config's Items/Workers/Apply shape exactly, plus the fault
// script this slice adds, so a fault run and its fault-free baseline can be
// built from the same scripted delivery order.
type Config struct {
	// Items is the scripted delivery order of work items. Build this with
	// schedulereplay.LoadWorkItems and one of its Schedule* helpers, so a fault
	// run and its baseline draw from the same fixture and ordering.
	Items []schedulereplay.WorkItem
	// Workers is the reducer worker count. expire-lease-mid-handler REQUIRES
	// Workers >= 2: the worker parked mid-handler cannot itself claim the
	// concurrent duplicate, so a single worker would deadlock the run forever.
	// RunFault refuses to start such a config (see Config.validate) rather than
	// hang.
	Workers int
	// Apply contributes a work item to the shared graph. Defaults to
	// schedulereplay.ApplyCanonical when nil.
	Apply schedulereplay.Applier
	// Script is the fault script to inject at the WorkSource/Executor seams.
	// An empty-faults script (Version: CurrentVersion, no Faults) is a valid
	// fault-free baseline run; a zero-value Script is rejected by Validate
	// because its Version is 0, not CurrentVersion.
	Script Script
}

// Report is RunFault's converged snapshot plus the accounting a caller needs
// to assert the Layer 4 acceptance: how many intents succeeded, and which
// intent IDs terminally failed.
type Report struct {
	// Snapshot is the converged canonical graph.
	Snapshot []byte
	// Acked is the number of successful intent completions, including any
	// fault-scripted redelivery that eventually succeeded.
	Acked int64
	// FailedIntentIDs lists every intent ID that landed in the sink's terminal
	// failed set, in the order Fail was observed. A correct fail-terminal
	// script produces exactly one entry naming the scripted intent; every
	// other fault class in this package always recovers, producing none.
	FailedIntentIDs []string
}

// validate rejects a Config this hermetic runner cannot honor without either
// deadlocking or silently no-op'ing a scripted fault.
func (c Config) validate() error {
	for _, f := range c.Script.Faults {
		if f.Kind == KindExpireLeaseMidHandler && c.Workers < 2 {
			return fmt.Errorf(
				"%s requires Config.Workers >= 2 (got %d): a single worker cannot claim its own concurrent duplicate, which would deadlock the run",
				KindExpireLeaseMidHandler, c.Workers)
		}
	}
	return nil
}

// RunFault drives cfg.Items through the real reducer.Service loop with the
// fault script's WorkSource/Executor decorators applied, drains to
// completion, and returns the converged canonical graph plus terminal
// accounting. Like schedulereplay.RunScheduleReport's awaitDrain, it refuses
// to report a partial drain: a pre-canceled or non-draining context returns an
// error, never a green empty snapshot.
func RunFault(ctx context.Context, cfg Config) (Report, error) {
	if err := cfg.validate(); err != nil {
		return Report{}, err
	}
	if err := cfg.Script.Validate(); err != nil {
		return Report{}, fmt.Errorf("fault replay: invalid script: %w", err)
	}

	apply := cfg.Apply
	if apply == nil {
		apply = schedulereplay.ApplyCanonical
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = 1
	}

	registry := make(map[string]schedulereplay.WorkItem, len(cfg.Items))
	intents := make([]reducer.Intent, 0, len(cfg.Items))
	available := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, item := range cfg.Items {
		registry[item.IntentID] = item
		intents = append(intents, reducer.Intent{
			IntentID:     item.IntentID,
			ScopeID:      "fault-replay",
			GenerationID: "fault-gen",
			SourceSystem: "faultreplay",
			Domain:       reducer.DomainCodeCallMaterialization,
			Cause:        "fault-replay",
			Status:       reducer.IntentStatusClaimed,
			EnqueuedAt:   available,
			AvailableAt:  available,
		})
	}

	graph := schedulereplay.NewGraph()
	applyExec := &canonicalApplyExecutor{registry: registry, graph: graph, apply: apply}
	scheduled := schedulereplay.NewScheduledWorkSource(intents)

	source, err := NewFaultingWorkSource(scheduled, cfg.Script)
	if err != nil {
		return Report{}, err
	}
	faultExec, err := NewFaultingExecutor(applyExec, cfg.Script, cfg.Items, source)
	if err != nil {
		return Report{}, err
	}
	sink := &faultingSink{}

	total := len(intents) + extraDrainCount(cfg.Script)

	svc := reducer.Service{
		PollInterval:   time.Millisecond,
		WorkSource:     source,
		Executor:       faultExec,
		WorkSink:       sink,
		Workers:        workers,
		BatchClaimSize: 4,
		Wait:           faultCtxAwareWait,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- svc.Run(runCtx) }()

	if err := awaitFaultDrain(runCtx, source, sink, total, runErr, cancel); err != nil {
		return Report{}, err
	}
	if execErr := applyExec.firstErr(); execErr != nil {
		return Report{}, execErr
	}

	snap, err := graph.Canonical()
	if err != nil {
		return Report{}, fmt.Errorf("fault replay: canonicalize graph: %w", err)
	}
	return Report{
		Snapshot:        snap,
		Acked:           sink.acked(),
		FailedIntentIDs: sink.failedIDs(),
	}, nil
}

// extraDrainCount reports how many additional intent completions (beyond one
// per scripted work item) a fault script requires before the run has truly
// drained: kill-worker-after-claim and expire-lease-mid-handler each deliver
// their targeted intent twice (both deliveries execute and ack), and the
// queue-retry lane of fail-graph-write-once-then-succeed fails once then
// succeeds once for its targeted intent. The executor-retry lane and
// fail-terminal contribute nothing extra: executor-retry never leaves a trace
// at the sink, and fail-terminal's single failure IS its one completion.
func extraDrainCount(s Script) int {
	extra := 0
	for _, f := range s.Faults {
		switch f.Kind {
		case KindKillWorkerAfterClaim, KindExpireLeaseMidHandler:
			extra++
		case KindFailGraphWriteOnceThenSucceed:
			if f.Target.Lane == LaneQueueRetry {
				extra++
			}
		}
	}
	return extra
}

// awaitFaultDrain blocks until every scripted intent (plus every fault-
// injected redelivery, per total) has been claimed and processed (acked or
// failed), the reducer loop exits on its own, or the context is canceled. It
// then cancels the loop and joins it, surfacing any non-cancellation error the
// loop returned. This is schedulereplay.awaitDrain's structure exactly, with
// one deliberate difference: it does NOT treat sink.failedCount() > 0 as an
// error, because a fault run's whole point is to exercise scripted failures
// (fail-terminal, or a queue-retry lane's transient Fail before recovery) that
// a fault-free schedule replay would never see.
func awaitFaultDrain(
	ctx context.Context,
	source *FaultingWorkSource,
	sink *faultingSink,
	total int,
	runErr <-chan error,
	cancel context.CancelFunc,
) error {
	ticker := time.NewTicker(200 * time.Microsecond)
	defer ticker.Stop()
	drained := func() bool { return source.Drained() && sink.processed() >= int64(total) }
	for {
		select {
		case err := <-runErr:
			if e := faultLoopExitError(err); e != nil {
				return e
			}
			if !drained() {
				return fmt.Errorf("fault replay loop exited before draining: source_drained=%v processed=%d/%d",
					source.Drained(), sink.processed(), total)
			}
			return nil
		case <-ctx.Done():
			cancel()
			<-runErr
			if drained() {
				return nil
			}
			return fmt.Errorf("fault replay canceled before drain (processed=%d/%d): %w",
				sink.processed(), total, ctx.Err())
		case <-ticker.C:
			if drained() {
				cancel()
				return faultLoopExitError(<-runErr)
			}
		}
	}
}

// faultLoopExitError normalizes the reducer loop's return: a context
// cancellation is the expected stop signal once work has drained, not a
// failure.
func faultLoopExitError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// faultCtxAwareWait is reducer.Service.Wait wired to respect context
// cancellation during the empty-queue poll backoff, matching
// schedulereplay's unexported ctxAwareWait.
func faultCtxAwareWait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("fault replay wait canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// canonicalApplyExecutor is the innermost Executor: it projects a claimed
// intent's registered work item into the shared graph, exactly like
// schedulereplay's unexported graphExecutor (duplicated here rather than
// imported since schedulereplay does not export it, and this package's
// FaultingExecutor needs to wrap something at the reducer.Executor seam).
type canonicalApplyExecutor struct {
	registry map[string]schedulereplay.WorkItem
	graph    *schedulereplay.Graph
	apply    schedulereplay.Applier

	mu  sync.Mutex
	err error
}

func (e *canonicalApplyExecutor) Execute(ctx context.Context, intent reducer.Intent) (reducer.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return reducer.Result{}, e.recordErrLocked(fmt.Errorf("fault replay execute canceled: %w", err))
	}
	item, ok := e.registry[intent.IntentID]
	if !ok {
		return reducer.Result{}, e.recordErrLocked(fmt.Errorf("no work item registered for intent %q", intent.IntentID))
	}
	e.apply(e.graph, item)
	return reducer.Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   reducer.ResultStatusSucceeded,
	}, nil
}

func (e *canonicalApplyExecutor) recordErrLocked(err error) error {
	if e.err == nil {
		e.err = err
	}
	return err
}

func (e *canonicalApplyExecutor) firstErr() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// faultingSink acknowledges or fails intents and tracks two independent
// things RunFault needs:
//
//   - a monotonic raw completion count (ackedCount + failedEventCount) that
//     never decreases, so awaitFaultDrain's "has every scripted completion,
//     including every fault-injected redelivery, happened yet" check is safe
//     against a Fail-then-later-Ack sequence for the same intent; and
//   - the reconciled terminalFailed set: an intent added on Fail is removed
//     again if a later Ack lands for the same intent ID (the queue-retry lane
//     fails once, transiently, then recovers -- that is not a dead letter).
//     Only an intent that fails and never later acks (fail-terminal) survives
//     in terminalFailed to the end of the run, which is what Report.
//     FailedIntentIDs must reflect: "dead letters appear only where the
//     script injected a terminal failure."
//
// Unlike schedulereplay's countingSink, a non-zero terminalFailed set is not
// itself an error here -- see awaitFaultDrain's doc comment.
type faultingSink struct {
	ackedCount       atomic.Int64
	failedEventCount atomic.Int64

	mu             sync.Mutex
	terminalFailed []string
}

func (s *faultingSink) Ack(_ context.Context, intent reducer.Intent, _ reducer.Result) error {
	s.ackedCount.Add(1)
	s.mu.Lock()
	for i, id := range s.terminalFailed {
		if id == intent.IntentID {
			s.terminalFailed = append(s.terminalFailed[:i], s.terminalFailed[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *faultingSink) Fail(_ context.Context, intent reducer.Intent, _ error) error {
	s.failedEventCount.Add(1)
	s.mu.Lock()
	s.terminalFailed = append(s.terminalFailed, intent.IntentID)
	s.mu.Unlock()
	return nil
}

func (s *faultingSink) acked() int64 { return s.ackedCount.Load() }

func (s *faultingSink) failedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.terminalFailed))
	copy(out, s.terminalFailed)
	return out
}

// processed reports how many raw completion events (acks plus fails,
// including a later-reconciled Fail) have happened -- the monotonic signal
// that the (fault-adjusted) schedule has fully drained. It deliberately does
// NOT subtract a reconciled Fail: a queue-retry lane's Fail-then-Ack is two
// real events extraDrainCount already budgeted for, not one.
func (s *faultingSink) processed() int64 {
	return s.ackedCount.Load() + s.failedEventCount.Load()
}
