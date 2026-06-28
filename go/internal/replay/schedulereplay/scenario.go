// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Config parameterizes one schedule-replay run.
type Config struct {
	// Items is the scripted delivery order of work items (already ordered and,
	// optionally, containing duplicates).
	Items []WorkItem
	// Workers is the reducer worker count. 1 drives the deterministic sequential
	// claim loop; >1 drives the real concurrent ClaimBatch path.
	Workers int
	// Apply contributes a work item to the shared graph. Defaults to
	// ApplyCanonical when nil.
	Apply Applier
}

// RunSchedule drives the recorded work items through the real reducer service
// loop, in the scripted delivery order, and returns the canonical graph snapshot
// the run converges on.
func RunSchedule(ctx context.Context, cfg Config) ([]byte, error) {
	snap, _, err := RunScheduleReport(ctx, cfg)
	return snap, err
}

// RunScheduleReport is RunSchedule plus the number of ClaimBatch invocations, so
// a concurrency scenario can prove the in-memory BatchWorkSource batch path
// actually ran.
func RunScheduleReport(ctx context.Context, cfg Config) (snapshot []byte, claimBatchCalls int, err error) {
	apply := cfg.Apply
	if apply == nil {
		apply = ApplyCanonical
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = 1
	}

	registry := make(map[string]WorkItem, len(cfg.Items))
	intents := make([]reducer.Intent, 0, len(cfg.Items))
	available := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, item := range cfg.Items {
		registry[item.IntentID] = item
		intents = append(intents, reducer.Intent{
			IntentID:     item.IntentID,
			ScopeID:      "replay-schedule",
			GenerationID: "replay-gen",
			SourceSystem: "replay",
			Domain:       reducer.DomainCodeCallMaterialization,
			Cause:        "schedule-replay",
			Status:       reducer.IntentStatusClaimed,
			EnqueuedAt:   available,
			AvailableAt:  available,
		})
	}

	graph := NewGraph()
	exec := &graphExecutor{registry: registry, graph: graph, apply: apply}
	sink := &countingSink{}
	source := NewScheduledWorkSource(intents)

	svc := reducer.Service{
		PollInterval:   time.Millisecond,
		WorkSource:     source,
		Executor:       exec,
		WorkSink:       sink,
		Workers:        workers,
		BatchClaimSize: 4,
		Wait:           ctxAwareWait,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- svc.Run(runCtx) }()

	total := len(intents)
	if err := awaitDrain(runCtx, source, sink, total, runErr, cancel); err != nil {
		return nil, source.ClaimBatchCalls(), err
	}
	if execErr := exec.firstErr(); execErr != nil {
		return nil, source.ClaimBatchCalls(), execErr
	}
	if f := sink.failed(); f > 0 {
		return nil, source.ClaimBatchCalls(),
			fmt.Errorf("schedule replay had %d failed intent(s); refusing to snapshot a partial graph", f)
	}

	snap, err := graph.Canonical()
	if err != nil {
		return nil, source.ClaimBatchCalls(), err
	}
	return snap, source.ClaimBatchCalls(), nil
}

// awaitDrain blocks until every scripted intent has been claimed and acked, the
// reducer loop exits on its own, or the context is canceled. It then cancels the
// loop and joins it, surfacing any non-cancellation error the loop returned.
func awaitDrain(
	ctx context.Context,
	source *ScheduledWorkSource,
	sink *countingSink,
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
			// The loop exited on its own. A real error wins. Otherwise the loop
			// stopped because its context was canceled — that is only success if
			// every scripted intent actually drained; if not, fail loudly rather
			// than snapshot a partial graph and report a green gate.
			if e := loopExitError(err); e != nil {
				return e
			}
			if !drained() {
				return fmt.Errorf("schedule replay loop exited before draining: drained=%v acked=%d/%d",
					source.Drained(), sink.acked(), total)
			}
			return nil
		case <-ctx.Done():
			cancel()
			<-runErr
			if drained() {
				return nil
			}
			return fmt.Errorf("schedule replay canceled before drain (acked=%d/%d): %w",
				sink.acked(), total, ctx.Err())
		case <-ticker.C:
			if drained() {
				cancel()
				return loopExitError(<-runErr)
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

func ctxAwareWait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("schedule replay wait canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// graphExecutor projects each claimed intent's work item into the shared graph.
// It serializes graph mutations behind a mutex so the concurrent reducer worker
// pool can share one graph safely.
type graphExecutor struct {
	registry map[string]WorkItem
	graph    *Graph
	apply    Applier

	mu  sync.Mutex
	err error
}

func (e *graphExecutor) Execute(ctx context.Context, intent reducer.Intent) (reducer.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Record every failure in firstErr: the reducer loop fails-and-continues
	// (it does not abort the drain on an executor error), so a failed intent
	// would otherwise leave the graph silently incomplete while the run still
	// looks drained. firstErr is the loud-failure backstop.
	if err := ctx.Err(); err != nil {
		return reducer.Result{}, e.recordErrLocked(fmt.Errorf("schedule replay execute canceled: %w", err))
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

// recordErrLocked stores the first execute error and returns it. The caller must
// hold e.mu.
func (e *graphExecutor) recordErrLocked(err error) error {
	if e.err == nil {
		e.err = err
	}
	return err
}

func (e *graphExecutor) firstErr() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// countingSink acknowledges intents and counts completions so the runner knows
// when the schedule has fully drained. Acks and failures are counted separately:
// processed() (acked+failed) tells the runner the loop is done, while a non-zero
// failed() means the run did not project cleanly and must not be reported green.
type countingSink struct {
	ackedCount  atomic.Int64
	failedCount atomic.Int64
}

func (s *countingSink) Ack(_ context.Context, _ reducer.Intent, _ reducer.Result) error {
	s.ackedCount.Add(1)
	return nil
}

func (s *countingSink) Fail(_ context.Context, _ reducer.Intent, _ error) error {
	s.failedCount.Add(1)
	return nil
}

func (s *countingSink) AckBatch(_ context.Context, intents []reducer.Intent, _ []reducer.Result) error {
	s.ackedCount.Add(int64(len(intents)))
	return nil
}

func (s *countingSink) acked() int64 {
	return s.ackedCount.Load()
}

func (s *countingSink) failed() int64 {
	return s.failedCount.Load()
}

// processed reports how many intents the loop has finished (acked or failed),
// which is the signal that the schedule has fully drained.
func (s *countingSink) processed() int64 {
	return s.ackedCount.Load() + s.failedCount.Load()
}
