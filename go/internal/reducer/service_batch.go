// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer //nolint:filelength // 509 lines (base 476): the #8625 mid-handoff and post-receive drop records live inline at their select/branch sites; extracting them would split each drop's ownership proof from its branch, and a file split is out of scope for this telemetry-only change.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

type batchAckItem struct {
	intent    reducercontract.Intent
	result    reducercontract.Result
	duration  float64
	queueWait float64
	workerID  int
}

// runBatchConcurrent uses a single claimer goroutine to claim batches of work
// and hand them only to ready worker goroutines. A separate acker goroutine
// batches acknowledgments. This reduces Postgres round-trips from O(items) to
// O(items/batchSize) without preclaiming work that is not yet heartbeat-owned.
func (s Service) runBatchConcurrent(
	ctx context.Context,
	batchSource BatchWorkSource,
	batchSink BatchWorkSink,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	batchSize := s.batchClaimSize()

	type workItem struct {
		intent reducercontract.Intent
	}

	workCh := make(chan workItem)
	ackCh := make(chan batchAckItem, batchSize*2)
	workerReady := make(chan struct{}, s.Workers)

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

	// Claimer goroutine: claims only as many items as workers are ready to
	// execute. Reducer leases are heartbeat-protected by the worker execution
	// path; preclaiming into a buffered queue can let leases expire before a
	// worker starts heartbeating them.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(workCh)

		idleWorkers := 0
		for {
			if ctx.Err() != nil {
				return
			}

			for idleWorkers == 0 {
				select {
				case <-workerReady:
					idleWorkers++
				case <-ctx.Done():
					return
				}
			}
			for idleWorkers < s.Workers {
				select {
				case <-workerReady:
					idleWorkers++
				default:
					goto readyDrained
				}
			}

		readyDrained:
			claimLimit := batchSize
			if claimLimit > idleWorkers {
				claimLimit = idleWorkers
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
					return
				}
				appendErr(fmt.Errorf("batch claim reducer work: %w", err))
				return
			}

			if len(intents) == 0 {
				if err := s.wait(ctx, s.pollInterval()); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
						return
					}
					appendErr(fmt.Errorf("wait for reducer work: %w", err))
					return
				}
				continue
			}

			if ctx.Err() != nil {
				// The claim won a race with shutdown: the batch is claimed
				// but no worker may start heartbeat-owned work on a
				// cancelled run. Never execute or ack it — the unrenewed
				// lease expires for reclaim (#4464) — and record the unknown
				// outcome so the claimed batch stays observable instead of
				// vanishing between claim and handoff (#8623).
				claimElapsed := time.Since(claimStart).Seconds()
				droppedAt := time.Now()
				for _, intent := range intents {
					s.recordBatchAckOutcome(ctx, batchAckItem{
						intent:    intent,
						queueWait: reducerQueueWaitSeconds(droppedAt, intent.AvailableAt),
						duration:  claimElapsed,
						// -1: never assigned to a worker; dropped between
						// claim and handoff.
						workerID: -1,
					}, ackStatusOutcomeUnknown, ctx.Err())
				}
				return
			}

			for i, intent := range intents {
				select {
				case workCh <- workItem{intent: intent}:
					idleWorkers--
				case <-ctx.Done():
					// Cancel landed mid-handoff: items already sent belong
					// to their workers, but the current and remaining
					// unhanded items are still owned here (the unbuffered
					// workCh rendezvous never completed for them). Never
					// execute or ack them — the unrenewed leases expire for
					// reclaim (#4464) — and record the unknown outcome so
					// the claimed remainder stays observable instead of
					// vanishing (#8625).
					claimElapsed := time.Since(claimStart).Seconds()
					droppedAt := time.Now()
					for _, dropped := range intents[i:] {
						s.recordBatchAckOutcome(ctx, batchAckItem{
							intent:    dropped,
							queueWait: reducerQueueWaitSeconds(droppedAt, dropped.AvailableAt),
							duration:  claimElapsed,
							// -1: never handed to a worker; dropped mid-handoff.
							workerID: -1,
						}, ackStatusOutcomeUnknown, ctx.Err())
					}
					return
				}
			}
		}
	}()

	// Worker goroutines: execute intents and send results to acker.
	for i := 0; i < s.Workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case workerReady <- struct{}{}:
				case <-ctx.Done():
					return
				}

				var wi workItem
				select {
				case next, ok := <-workCh:
					if !ok {
						return
					}
					wi = next
				case <-ctx.Done():
					return
				}
				if ctx.Err() != nil {
					// Shutdown landed after the receive rendezvous completed
					// but before execution: the claimer no longer owns this
					// item and no worker work ran on it. Never execute or
					// ack it — the unrenewed lease expires for reclaim
					// (#4464) — and record the unknown outcome so the
					// received-but-unexecuted item stays observable instead
					// of vanishing (#8625).
					receivedAt := time.Now()
					s.recordBatchAckOutcome(ctx, batchAckItem{
						intent:    wi.intent,
						queueWait: reducerQueueWaitSeconds(receivedAt, wi.intent.AvailableAt),
						// Never executed: no handler duration to report.
						workerID: workerID,
					}, ackStatusOutcomeUnknown, ctx.Err())
					return
				}

				ackItem, needsAck, err := s.executeAndReport(ctx, wi.intent, workerID)
				if err != nil {
					// Execute failures that require a Fail() call are handled
					// inside executeAndReport. A returned error means the Fail
					// itself broke — that's fatal.
					appendErr(err)
					return
				}
				if !needsAck {
					// executeAndReport already terminalized this intent through
					// WorkSink.Fail. Acking it as well would match zero rows
					// (Fail clears lease_owner and moves status off
					// 'claimed'/'running'), which the container_image_identity
					// and ci_cd_run_correlation ack paths report as
					// ErrReducerClaimRejected. Skipping it avoids a second
					// terminal transition for work the handler already failed.
					continue
				}
				if ctx.Err() != nil {
					s.recordBatchAckOutcome(ctx, ackItem, ackStatusOutcomeUnknown, ctx.Err())
					return
				}

				select {
				case ackCh <- ackItem:
				case <-ctx.Done():
					s.recordBatchAckOutcome(ctx, ackItem, ackStatusOutcomeUnknown, ctx.Err())
					return
				}
			}
		}()
	}

	// Acker goroutine: collects results and batches acks.
	ackDone := make(chan struct{})
	go func() {
		defer close(ackDone)

		var pending []batchAckItem
		flushTimer := time.NewTimer(100 * time.Millisecond)
		defer flushTimer.Stop()

		flush := func() {
			if len(pending) == 0 {
				return
			}
			if ctx.Err() != nil {
				for _, item := range pending {
					s.recordBatchAckOutcome(ctx, item, ackStatusOutcomeUnknown, ctx.Err())
				}
				pending = pending[:0]
				return
			}

			intents := make([]reducercontract.Intent, len(pending))
			results := make([]reducercontract.Result, len(pending))
			for i, item := range pending {
				intents[i] = item.intent
				results[i] = item.result
			}

			if err := s.ackBatchRetryingTransient(ctx, batchSink, intents, results); err != nil {
				unknown := s.recordBatchAckError(ctx, pending, err)
				if ctx.Err() == nil {
					switch {
					case errors.Is(err, errAckAbandonedToLeaseExpiry):
						// Claims stay leased and expire for reclaim; the run
						// keeps draining (#7267). Already logged by the retrier.
					case errors.Is(err, ErrExecutionClaimRejected):
						// A fully attributed supersession batch already
						// logged each dropped claim with its work item id;
						// the fence line stays for batches with
						// unattributed items.
						if unknown > 0 {
							s.logReducerAckClaimRejected(ctx, nil, len(intents), err)
						}
					default:
						appendErr(fmt.Errorf("batch ack reducer work: %w", err))
					}
				}
			} else {
				for _, item := range pending {
					s.recordBatchAckOutcome(ctx, item, "succeeded", nil)
				}
			}
			pending = pending[:0]
		}

		for {
			select {
			case item, ok := <-ackCh:
				if !ok {
					flush()
					return
				}
				pending = append(pending, item)
				if len(pending) >= batchSize {
					flush()
					if !flushTimer.Stop() {
						select {
						case <-flushTimer.C:
						default:
						}
					}
					flushTimer.Reset(100 * time.Millisecond)
				}
			case <-flushTimer.C:
				flush()
				flushTimer.Reset(100 * time.Millisecond)
			case <-ctx.Done():
				for _, item := range pending {
					s.recordBatchAckOutcome(ctx, item, ackStatusOutcomeUnknown, ctx.Err())
				}
				for item := range ackCh {
					s.recordBatchAckOutcome(ctx, item, ackStatusOutcomeUnknown, ctx.Err())
				}
				return
			}
		}
	}()

	// Wait for claimer to finish, then workers, then close ack channel.
	// The claimer closes workCh when done, which causes workers to drain.
	wg.Wait()
	close(ackCh)
	<-ackDone

	return errors.Join(errs...)
}

func (s Service) recordBatchAckOutcome(ctx context.Context, item batchAckItem, status string, ackErr error) {
	result := item.result
	if status != "succeeded" {
		result = reducercontract.Result{}
	}
	s.recordReducerResult(ctx, item.intent, result, item.duration, item.queueWait, status, item.workerID, ackErr)
}

func (s Service) ackReducerWork(
	ctx context.Context,
	intent reducercontract.Intent,
	result reducercontract.Result,
	duration float64,
	queueWait float64,
	status string,
	workerID int,
) error {
	err := s.ackSingleRetryingTransient(ctx, intent, result)
	if err == nil {
		s.recordReducerResult(ctx, intent, result, duration, queueWait, status, workerID, nil)
		return nil
	}
	if errors.Is(err, ErrExecutionClaimRejected) {
		s.recordReducerResult(ctx, intent, reducercontract.Result{}, duration, queueWait, "ack_claim_rejected", workerID, err)
		s.logReducerAckClaimRejected(ctx, &intent, 0, err)
		return nil
	}
	if errors.Is(err, errAckAbandonedToLeaseExpiry) {
		// The claim stays leased and is reclaimed at lease expiry, so the
		// run keeps draining (#7444); the retrier already logged the attempt.
		s.recordReducerResult(ctx, intent, reducercontract.Result{}, duration, queueWait, ackStatusAbandonedToLeaseExpiry, workerID, err)
		return nil
	}
	if errors.Is(err, errAckRetryInterrupted) {
		// Shutdown interrupted the retry backoff: the ack did not commit and
		// the claim expires for reclaim, which is a stopping process rather
		// than a failed run, matching the batch path.
		s.recordReducerResult(ctx, intent, reducercontract.Result{}, duration, queueWait, ackStatusOutcomeUnknown, workerID, err)
		return nil
	}

	s.recordReducerResult(ctx, intent, reducercontract.Result{}, duration, queueWait, "ack_failed", workerID, err)
	return fmt.Errorf("ack reducer work: %w", err)
}

func (s Service) logReducerAckClaimRejected(
	ctx context.Context,
	intent *reducercontract.Intent,
	batchSize int,
	err error,
) {
	if s.Logger == nil {
		return
	}

	message := "reducer ack rejected stale claim"
	attrs := []any{
		log.Queue("reducer"),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
		telemetry.FailureClassAttr("execution_claim_rejected"),
		log.Err(err),
	}
	if intent != nil {
		partitionKey := ""
		if len(intent.EntityKeys) > 0 {
			partitionKey = intent.EntityKeys[0]
		}
		for _, attr := range telemetry.DomainAttrs(string(intent.Domain), partitionKey) {
			attrs = append(attrs, attr)
		}
		attrs = append(attrs, log.IntentID(intent.IntentID))
		attrs = append(attrs, log.Status("ack_claim_rejected"))
	} else {
		message = "reducer batch ack rejected stale claim"
		attrs = append(attrs, slog.Int("batch_size", batchSize))
	}

	s.Logger.WarnContext(ctx, message, attrs...)
}

// executeAndReport runs one intent through the executor and reports failures.
// It defers success telemetry to the batch acker, which knows whether AckBatch
// accepted every item. On execution failure, it calls WorkSink.Fail and returns
// nil. On Fail or heartbeat errors, it returns a non-nil error (fatal).
//
// The second return is whether the caller still owes this intent an
// acknowledgment. When err is nil, it is false when WorkSink.Fail has already
// terminalized the row or the execution claim was rejected, so the caller must
// not ack it.
// The non-nil-error returns carry a don't-care false value: Fail may not have
// terminalized the row at all, and on the heartbeat-error path the executor
// actually succeeded and the row is still claimed. The per-item path in
// service.go holds the same contract by returning early after its own Fail
// call.
func (s Service) executeAndReport(ctx context.Context, intent reducercontract.Intent, workerID int) (batchAckItem, bool, error) {
	start := time.Now()
	queueWait := reducerQueueWaitSeconds(start, intent.AvailableAt)

	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanReducerRun)
		defer span.End()
	}

	execCtx, stopHeartbeat := s.startHeartbeat(ctx, intent, workerID)
	defer func() {
		_ = stopHeartbeat()
	}()

	execCtx = WithQuarantineWriter(execCtx, s.QuarantineWriter)
	execStart := time.Now()
	result, err, abandoned := s.executeHandler(execCtx, ctx, intent)
	if abandoned {
		return s.failAbandonedBatchHandler(ctx, intent, err, start, execStart, queueWait, workerID, stopHeartbeat)
	}
	s.recordReducerHandlerDuration(ctx, intent, time.Since(execStart).Seconds())
	duration := time.Since(start).Seconds()
	if err != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			err = errors.Join(err, heartbeatErr)
		}
		if errors.Is(err, ErrExecutionClaimRejected) {
			s.recordReducerResult(ctx, intent, reducercontract.Result{}, duration, queueWait, "lease_lost_during_execution", workerID, err)
			return batchAckItem{}, false, nil
		}
		failErr := s.WorkSink.Fail(ctx, intent, err)
		if failErr != nil && errors.Is(failErr, ErrExecutionClaimRejected) {
			// A Fail rejected by the lease fence is a lost claim, not a broken
			// sink: record it once, drop the item and keep the batch draining
			// (#7142).
			s.recordReducerResult(ctx, intent, reducercontract.Result{}, duration, queueWait, "lease_lost_during_execution", workerID, failErr)
			return batchAckItem{}, false, nil
		}
		s.recordReducerResult(ctx, intent, reducercontract.Result{}, duration, queueWait, "failed", workerID, err)
		if failErr != nil {
			return batchAckItem{}, false, errors.Join(err, fmt.Errorf("fail reducer work: %w", failErr))
		}
		return batchAckItem{}, false, nil
	}

	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		s.recordReducerResult(ctx, intent, reducercontract.Result{}, duration, queueWait, "ack_failed", workerID, heartbeatErr)
		return batchAckItem{}, false, fmt.Errorf("heartbeat reducer work: %w", heartbeatErr)
	}

	return batchAckItem{
		intent:    intent,
		result:    result,
		duration:  duration,
		queueWait: queueWait,
		workerID:  workerID,
	}, true, nil
}
