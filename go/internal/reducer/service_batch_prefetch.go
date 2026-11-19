// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// batchPrefetchItem is one claimed intent waiting in the lookahead buffer.
// The hold heartbeat owns the lease from ClaimBatch until a worker pops the
// item; claimedAt bounds the buffer dwell the dwell histogram reports.
type batchPrefetchItem struct {
	intent    reducercontract.Intent
	claimedAt time.Time
	stopHold  reducerHeartbeatStop
}

// runBatchConcurrentPrefetch claims batches of work into a bounded FIFO
// buffer of hold-heartbeated items instead of handing each claim directly to
// a ready worker. The claimer is still the only claimer and the acker is
// still the only acker; the buffer sits between them.
//
// Lease safety comes from the hold heartbeat, not from the rendezvous: the
// claimer starts a hold via startHeartbeat immediately after ClaimBatch, so
// a buffered item's lease keeps renewing until a worker pops it. The claimer
// never claims more than free buffer slots, and it parks on the pop
// notifications instead of spinning when the buffer is full. Fairness is
// strict FIFO in ClaimBatch return order with
// overtake bound 0: a not-yet-claimed item waits at most B buffered items
// (positions), i.e. at most the time workers need to drain B items.
//
// The pop ordering is STOP hold (which blocks until the hold loop exits)
// THEN the worker heartbeat inside executeAndReport, so two heartbeat loops
// for one intent never overlap. A hold that fails at any point evicts the
// item: it is never executed, acked, or failed, and the unrenewed lease
// expires for reclaim (#4464).
func (s Service) runBatchConcurrentPrefetch(
	ctx context.Context,
	batchSource BatchWorkSource,
	batchSink BatchWorkSink,
	depth int,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	batchSize := s.batchClaimSize()

	if s.batchOccupancy == nil {
		s.batchOccupancy = newBatchWorkerOccupancy(s.Workers)
	}

	bufCh := make(chan batchPrefetchItem, depth)
	ackCh := make(chan batchAckItem, batchSize*2)
	// slotFreed carries pop notifications from workers to the claimer: one
	// send attempt per pop, so a received notification means at least one
	// pop happened (dropped sends only coalesce). Sends are non-blocking;
	// the claimer always rechecks length itself and never relies on a 1:1
	// pop-to-notification correspondence,
	// so a worker never stalls a popped item between its hold stop and its
	// worker heartbeat with the lease unrenewed.
	slotFreed := make(chan struct{}, s.Workers)

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	appendErr := func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
		cancel()
	}

	// Claimer goroutine: claims up to free-buffer-slots items and parks them
	// in the buffer with a hold heartbeat each. Every buffered item is
	// hold-owned here until a worker pops it. The bound uses free slots
	// alone, never worker readiness: a pre-pop ready credit goes stale when
	// its worker pops before the claimer drains it, and the stale credit
	// plus the freed slot would admit one claim too many. Free slots are
	// exact — every claimed item reserves its slot — and the hold heartbeat
	// (not worker readiness) is what keeps a parked lease alive.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(bufCh)
		s.claimPrefetchBatches(ctx, batchSource, batchSize, bufCh, slotFreed, appendErr)
	}()

	// Worker goroutines: pop buffered intents in FIFO order, stop the hold,
	// then execute exactly like the rendezvous path.
	for i := 0; i < s.Workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if !s.consumePrefetchedItem(ctx, bufCh, slotFreed, workerID, ackCh, appendErr) {
					return
				}
			}
		}()
	}

	// Acker goroutine: collects results and batches acks (shared with the
	// rendezvous path; see runBatchAckLoop).
	ackDone := s.runBatchAckLoop(ctx, batchSink, batchSize, ackCh, appendErr)

	// Wait for claimer to finish, then workers, then close ack channel.
	// The claimer closes bufCh when done, which causes workers to drain.
	wg.Wait()
	close(ackCh)
	<-ackDone

	return errors.Join(errs...)
}

// claimPrefetchBatches is the claimer loop for the prefetch path: claim up
// to free-buffer-slots items and hand each batch to bufferPrefetchedBatch
// for hold-start and insert. It owns every claimed item until a worker pops
// it or the run ends; every return path drains the buffer first so no
// claimed item is left unowned.
func (s Service) claimPrefetchBatches(
	ctx context.Context,
	batchSource BatchWorkSource,
	batchSize int,
	bufCh chan batchPrefetchItem,
	slotFreed <-chan struct{},
	appendErr func(error),
) {
	for {
		if ctx.Err() != nil {
			s.drainPrefetchBuffer(ctx, bufCh)
			return
		}

		// Block only while the buffer is full. A slotFreed notification
		// means at least one pop happened; a missed send only coalesces
		// with a pending wakeup, and every wakeup rechecks the length,
		// so the claimer never relies on a 1:1 correspondence. The ctx
		// arm keeps a full buffer from deadlocking shutdown.
		if len(bufCh) == cap(bufCh) {
			select {
			case <-slotFreed:
			case <-ctx.Done():
				s.drainPrefetchBuffer(ctx, bufCh)
				return
			}
			continue
		}
		claimLimit := batchSize
		if free := cap(bufCh) - len(bufCh); free < claimLimit {
			claimLimit = free
		}

		claimStart := time.Now()
		intents, err := batchSource.ClaimBatch(ctx, claimLimit)
		if s.Instruments != nil {
			s.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
				attribute.String("queue", "reducer"),
				attribute.String("mode", "batch"),
			))
			s.Instruments.BatchClaimSize.Record(ctx, int64(len(intents)), metric.WithAttributes(
				attribute.String("queue", "reducer"),
			))
		}
		if err != nil {
			if ctx.Err() != nil {
				s.drainPrefetchBuffer(ctx, bufCh)
				return
			}
			appendErr(fmt.Errorf("batch claim reducer work: %w", err))
			s.drainPrefetchBuffer(ctx, bufCh)
			return
		}

		if len(intents) == 0 {
			if err := s.wait(ctx, s.pollInterval()); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					s.drainPrefetchBuffer(ctx, bufCh)
					return
				}
				appendErr(fmt.Errorf("wait for reducer work: %w", err))
				s.drainPrefetchBuffer(ctx, bufCh)
				return
			}
			continue
		}

		if ctx.Err() != nil {
			// The claim won a race with shutdown: the batch is claimed
			// but no worker may start heartbeat-owned work on a
			// cancelled run (#8623).
			s.dropUnbufferedPrefetchClaims(ctx, intents, claimStart, bufCh)
			return
		}
		if !s.bufferPrefetchedBatch(ctx, intents, claimStart, bufCh) {
			return
		}
	}
}

// bufferPrefetchedBatch starts a hold per intent and inserts each item,
// evicting holds that are already dead at insert time. It reports whether
// the run continues: false means shutdown cut the buffering short, the
// remainder is already dropped and the buffer drained, and the caller must
// return.
func (s Service) bufferPrefetchedBatch(
	ctx context.Context,
	intents []reducercontract.Intent,
	claimStart time.Time,
	bufCh chan batchPrefetchItem,
) bool {
	for i, intent := range intents {
		if ctx.Err() != nil {
			// Cancel landed mid-buffering: items already in the buffer
			// belong to the drain, but the current and remaining
			// unbuffered items are still owned here (#8625).
			s.dropUnbufferedPrefetchClaims(ctx, intents[i:], claimStart, bufCh)
			return false
		}
		// Start the hold before taking a buffer slot: a lease whose
		// synchronous pre-heartbeat already failed never occupies
		// the buffer. startHeartbeat cancels the hold context on a
		// pre-heartbeat failure, so a non-nil hold error without a
		// cancelled run means the hold is already dead.
		holdCtx, stopHold := s.startHeartbeat(ctx, intent, batchDropUnassignedWorkerID)
		if holdCtx.Err() != nil {
			holdErr := stopHold()
			s.evictPrefetchItem(ctx, batchPrefetchItem{intent: intent, claimedAt: claimStart, stopHold: stopHold}, batchDropUnassignedWorkerID, holdErr)
			continue
		}
		item := batchPrefetchItem{intent: intent, claimedAt: time.Now(), stopHold: stopHold}
		s.recordPrefetchInsert(ctx)
		select {
		case bufCh <- item:
		case <-ctx.Done():
			// The slot was accounted but the item never entered the
			// buffer: undo the depth observation, then drop the
			// current and remaining items and drain what did enter.
			s.recordPrefetchRemove(ctx)
			_ = stopHold()
			s.dropUnbufferedPrefetchClaims(ctx, intents[i:], claimStart, bufCh)
			return false
		}
	}
	return true
}

// dropUnbufferedPrefetchClaims records the unknown outcome for claimed
// intents that never entered the buffer, then drains the buffer. The run is
// ending: none of these items executes or acks — the unrenewed leases
// expire for reclaim (#4464) — and the claimed batch stays observable
// instead of vanishing between claim and handoff (#8623).
func (s Service) dropUnbufferedPrefetchClaims(
	ctx context.Context,
	intents []reducercontract.Intent,
	claimStart time.Time,
	bufCh chan batchPrefetchItem,
) {
	claimElapsed := time.Since(claimStart).Seconds()
	droppedAt := time.Now()
	for _, intent := range intents {
		s.recordBatchAckOutcome(ctx, batchAckItem{
			intent:    intent,
			queueWait: reducerQueueWaitSeconds(droppedAt, intent.AvailableAt),
			duration:  claimElapsed,
			// Never buffered; no worker can pop it.
			workerID: batchDropUnassignedWorkerID,
		}, ackStatusOutcomeUnknown, ctx.Err())
	}
	s.drainPrefetchBuffer(ctx, bufCh)
}

// consumePrefetchedItem pops one buffered intent and drives it through the
// stop-hold, dwell, execute, and ack-send sequence. It reports whether the
// worker loop continues: false means the run ended (buffer closed,
// shutdown, or fatal error) and the caller must return.
func (s Service) consumePrefetchedItem(
	ctx context.Context,
	bufCh <-chan batchPrefetchItem,
	slotFreed chan<- struct{},
	workerID int,
	ackCh chan<- batchAckItem,
	appendErr func(error),
) bool {
	var item batchPrefetchItem
	select {
	case next, ok := <-bufCh:
		if !ok {
			return false
		}
		item = next
	case <-ctx.Done():
		return false
	}
	// The pop freed exactly one slot: notify the claimer. The send is
	// non-blocking (see slotFreed) so the popped item never waits here
	// with neither heartbeat running.
	select {
	case slotFreed <- struct{}{}:
	default:
	}
	s.recordPrefetchRemove(ctx)
	if s.afterWorkReceive != nil {
		s.afterWorkReceive()
	}
	if ctx.Err() != nil {
		// Shutdown landed after the pop: the claimer no longer owns this
		// item and no worker work ran on it. Stop the hold first so no
		// stray tick renews a lease we are abandoning, then never execute
		// or ack it — the unrenewed lease expires for reclaim (#4464) —
		// and record the unknown outcome so the popped-but-unexecuted item
		// stays observable (#8625).
		_ = item.stopHold()
		poppedAt := time.Now()
		s.recordBatchAckOutcome(ctx, batchAckItem{
			intent:    item.intent,
			queueWait: reducerQueueWaitSeconds(poppedAt, item.intent.AvailableAt),
			workerID:  workerID,
		}, ackStatusOutcomeUnknown, ctx.Err())
		return false
	}

	// Stop the hold before the worker heartbeat starts inside
	// executeAndReport: stop blocks until the hold loop exits, so the two
	// loops never overlap on one intent.
	if stopErr := item.stopHold(); stopErr != nil {
		// The hold died during the dwell. Evict: never execute, ack, or
		// fail — the lease already missed its renewal and expires for
		// reclaim (#4464) — and let the loop pop the next item.
		s.evictPrefetchItem(ctx, item, workerID, stopErr)
		return true
	}
	s.recordPrefetchDwell(ctx, item)

	ackItem, needsAck, err := s.executeAndReportTracked(ctx, item.intent, workerID)
	if err != nil {
		// Execute failures that require a Fail() call are handled inside
		// executeAndReport. A returned error means the Fail itself broke —
		// that's fatal.
		appendErr(err)
		return false
	}
	if !needsAck {
		// executeAndReport already terminalized this intent through
		// WorkSink.Fail. Acking it as well would match zero rows, which the
		// ack paths report as ErrReducerClaimRejected.
		return true
	}
	if ctx.Err() != nil {
		s.recordBatchAckOutcome(ctx, ackItem, ackStatusOutcomeUnknown, ctx.Err())
		return false
	}

	select {
	case ackCh <- ackItem:
		return true
	case <-ctx.Done():
		s.recordBatchAckOutcome(ctx, ackItem, ackStatusOutcomeUnknown, ctx.Err())
		return false
	}
}

// evictPrefetchItem records a buffered item whose hold heartbeat failed: the
// hold-failure counter fires once per eviction and the item records the
// unknown outcome with the worker that observed the dead hold (or the
// unassigned sentinel when the hold died before any worker popped it), so
// claimed-but-never-executed work stays observable instead of vanishing. The
// caller already stopped the hold; the lease is left unrenewed for the
// expired-lease reclaim path (#4464). Never executes, acks, or fails.
func (s Service) evictPrefetchItem(ctx context.Context, item batchPrefetchItem, workerID int, holdErr error) {
	if s.Instruments != nil {
		s.Instruments.BatchBufferHoldHeartbeatFailed.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(string(item.intent.Domain)),
		))
	}
	s.recordBatchAckOutcome(ctx, batchAckItem{
		intent:    item.intent,
		queueWait: reducerQueueWaitSeconds(time.Now(), item.intent.AvailableAt),
		duration:  time.Since(item.claimedAt).Seconds(),
		workerID:  workerID,
	}, ackStatusOutcomeUnknown, holdErr)
}

// drainPrefetchBuffer stops every hold still in the buffer and records the
// unknown outcome for each, so a shutdown drain never executes or acks
// buffered work. Each item is popped exactly once across the drain and any
// worker still racing its ctx arm: whoever pops an item owns its record.
// Stops are best-effort here — the run is ending — but every stopped hold
// still frees its depth slot.
func (s Service) drainPrefetchBuffer(ctx context.Context, bufCh <-chan batchPrefetchItem) {
	for {
		select {
		case item, ok := <-bufCh:
			if !ok {
				return
			}
			_ = item.stopHold()
			s.recordPrefetchRemove(ctx)
			s.recordBatchAckOutcome(ctx, batchAckItem{
				intent:    item.intent,
				queueWait: reducerQueueWaitSeconds(time.Now(), item.intent.AvailableAt),
				duration:  time.Since(item.claimedAt).Seconds(),
				// Still claimer-owned; never popped by a worker.
				workerID: batchDropUnassignedWorkerID,
			}, ackStatusOutcomeUnknown, ctx.Err())
		default:
			return
		}
	}
}

// recordPrefetchInsert marks one item entering the buffer on the depth
// gauge. Every insert pairs with exactly one remove (pop, eviction, or
// drain), so the gauge returns to zero when the buffer empties.
func (s Service) recordPrefetchInsert(ctx context.Context) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.BatchBufferDepth.Add(ctx, 1, metric.WithAttributes(
		attribute.String("queue", "reducer"),
	))
}

// recordPrefetchRemove marks one item leaving the buffer on the depth gauge.
func (s Service) recordPrefetchRemove(ctx context.Context) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.BatchBufferDepth.Add(ctx, -1, metric.WithAttributes(
		attribute.String("queue", "reducer"),
	))
}

// recordPrefetchDwell records one claim-to-execution-start observation per
// executed buffered item. Dropped items (evicted or drained) record none, so
// the histogram measures only work the buffer actually handed to a worker.
func (s Service) recordPrefetchDwell(ctx context.Context, item batchPrefetchItem) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.BatchBufferDwell.Record(ctx, time.Since(item.claimedAt).Seconds(), metric.WithAttributes(
		attribute.String("queue", "reducer"),
	))
}
