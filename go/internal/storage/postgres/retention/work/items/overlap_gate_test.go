// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	// overlapRetentionPace paces retention behind the workers: batch k after
	// retention's first deleting batch starts only once the workers have
	// processed k*overlapRetentionPace items. Retention therefore cannot
	// drain the seeded history before the workers run: about 60 deleting
	// windows need about 240 of the 300 live items processed.
	overlapRetentionPace = 4
	// overlapWorkerPace paces the workers behind retention: a worker takes
	// claim ticket n only once retention has committed
	// n/overlapWorkerPace batches since its first deleting batch, so the
	// workers cannot finish before retention has committed batches during
	// their phase.
	overlapWorkerPace = 10
	// overlapWait bounds every gate wait. Reaching it fails the proof
	// loudly instead of hanging it.
	overlapWait = 2 * time.Minute
)

// overlapMinBatches is the number of retention batches the gate guarantees
// commit while the workers run: the last claims (the ones that find no work)
// hold tickets of at least concurrencyLiveItems, and each waits for
// ticket/overlapWorkerPace batches.
func overlapMinBatches() int { return concurrencyLiveItems / overlapWorkerPace }

// overlapGate couples the retention loop and the lifecycle workers of
// TestPruneWorkItemsLiveDoesNotContendWithClaimLifecycle so they interleave
// by construction, not by timing:
//
//   - workers start only after retention committed a batch that deleted rows;
//   - retention runs at most overlapRetentionPace items' worth of batches
//     ahead of the workers;
//   - workers run at most overlapWorkerPace items per batch ahead of
//     retention.
//
// Neither side can deadlock the other: a worker at ticket n needs n/10
// batches, and batch n/10 needs only 0.4n processed items, which the
// workers reached before taking ticket n.
type overlapGate struct {
	processed *atomic.Int64
	deadline  time.Time

	started   chan struct{} // closed after retention's first deleting batch
	stopped   chan struct{} // closed when the retention loop exits
	finished  chan struct{} // closed when every worker returned
	startOnce sync.Once
	stopOnce  sync.Once

	batches atomic.Int64 // batches committed after started
	deleted atomic.Int64 // rows those batches deleted
	tickets atomic.Int64 // claim tickets handed to workers
}

func newOverlapGate(processed *atomic.Int64) *overlapGate {
	return &overlapGate{
		processed: processed,
		deadline:  time.Now().Add(overlapWait),
		started:   make(chan struct{}),
		stopped:   make(chan struct{}),
		finished:  make(chan struct{}),
	}
}

// waitFor polls cond until it holds. It returns nil early when release is
// closed, and an error when abort is closed first or the deadline passes.
func (g *overlapGate) waitFor(what string, cond func() bool, release, abort <-chan struct{}) error {
	for !cond() {
		select {
		case <-release:
			return nil
		case <-abort:
			if cond() {
				return nil
			}
			return fmt.Errorf("overlap gate: retention stopped before %s", what)
		case <-time.After(time.Millisecond):
		}
		if time.Now().After(g.deadline) {
			return fmt.Errorf("overlap gate: timed out after %s waiting for %s", overlapWait, what)
		}
	}
	return nil
}

func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// beforeRetentionBatch holds the next retention batch until the workers have
// caught up with it. Before the first deleting batch, retention runs freely.
func (g *overlapGate) beforeRetentionBatch() error {
	if !closed(g.started) {
		return nil
	}
	need := g.batches.Load() * overlapRetentionPace
	return g.waitFor(fmt.Sprintf("workers to process %d items", need),
		func() bool { return g.processed.Load() >= need }, g.finished, nil)
}

// retentionCommitted records one committed retention call.
func (g *overlapGate) retentionCommitted(result workflow.WorkItemRetentionResult) {
	if !closed(g.started) {
		if result.Deleted() > 0 {
			g.startOnce.Do(func() { close(g.started) })
		}
		return
	}
	g.deleted.Add(int64(result.Deleted()))
	g.batches.Add(int64(result.Batches))
}

// retentionStopped marks the retention loop as exited, so waiting workers
// fail at once instead of at the deadline.
func (g *overlapGate) retentionStopped() { g.stopOnce.Do(func() { close(g.stopped) }) }

// workersFinished reports whether finishWorkers ran.
func (g *overlapGate) workersFinished() bool { return closed(g.finished) }

// beforeClaim holds a worker until retention has started deleting and has
// committed enough batches for the worker's next claim ticket.
func (g *overlapGate) beforeClaim() error {
	if err := g.waitFor("retention's first deleting batch",
		func() bool { return closed(g.started) }, nil, g.stopped); err != nil {
		return err
	}
	ticket := g.tickets.Add(1) - 1
	need := ticket / overlapWorkerPace
	return g.waitFor(fmt.Sprintf("%d retention batches for claim ticket %d", need, ticket),
		func() bool { return g.batches.Load() >= need }, nil, g.stopped)
}

// finishWorkers releases the retention loop and returns the batches and
// deleted rows retention committed while the workers ran.
func (g *overlapGate) finishWorkers() (batches, deleted int64) {
	batches, deleted = g.batches.Load(), g.deleted.Load()
	close(g.finished)
	return batches, deleted
}

// recordMax raises *slot to value when value is larger.
func recordMax(slot *atomic.Int64, value int64) {
	for {
		current := slot.Load()
		if value <= current || slot.CompareAndSwap(current, value) {
			return
		}
	}
}
