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
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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
		intent Intent
	}
	type ackItem struct {
		intent Intent
		result Result
	}

	workCh := make(chan workItem)
	ackCh := make(chan ackItem, batchSize*2)
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

			for _, intent := range intents {
				select {
				case workCh <- workItem{intent: intent}:
					idleWorkers--
				case <-ctx.Done():
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
					return
				}

				result, needsAck, err := s.executeAndReport(ctx, wi.intent, workerID)
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
					// ErrReducerClaimRejected — a fatal error that cancels the
					// whole run over a routine, retryable handler failure.
					continue
				}

				select {
				case ackCh <- ackItem{intent: wi.intent, result: result}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Acker goroutine: collects results and batches acks.
	ackDone := make(chan struct{})
	go func() {
		defer close(ackDone)

		var pending []ackItem
		flushTimer := time.NewTimer(100 * time.Millisecond)
		defer flushTimer.Stop()

		flush := func() {
			if len(pending) == 0 {
				return
			}

			intents := make([]Intent, len(pending))
			results := make([]Result, len(pending))
			for i, item := range pending {
				intents[i] = item.intent
				results[i] = item.result
			}

			if err := batchSink.AckBatch(ctx, intents, results); err != nil {
				if ctx.Err() == nil {
					appendErr(fmt.Errorf("batch ack reducer work: %w", err))
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
				flush()
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

// executeAndReport runs one intent through the executor and reports the
// result. On execution failure, it calls WorkSink.Fail and returns nil. On
// Fail/Ack infrastructure errors, it returns a non-nil error (fatal).
//
// The second return is whether the caller still owes this intent an
// acknowledgment. When err is nil, it is false exactly when WorkSink.Fail has
// already terminalized the row, so the caller must not ack it a second time.
// The non-nil-error returns carry a don't-care false value: Fail may not have
// terminalized the row at all, and on the heartbeat-error path the executor
// actually succeeded and the row is still claimed. The per-item path in
// service.go holds the same contract by returning early after its own Fail
// call.
func (s Service) executeAndReport(ctx context.Context, intent Intent, workerID int) (Result, bool, error) {
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
	result, err := s.Executor.Execute(execCtx, intent)
	duration := time.Since(start).Seconds()
	status := "succeeded"

	if err != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			err = errors.Join(err, heartbeatErr)
		}
		status = "failed"
		s.recordReducerResult(ctx, intent, Result{}, duration, queueWait, status, workerID, err)
		if failErr := s.WorkSink.Fail(ctx, intent, err); failErr != nil {
			return Result{}, false, errors.Join(err, fmt.Errorf("fail reducer work: %w", failErr))
		}
		return Result{Status: ResultStatusFailed}, false, nil
	}

	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		s.recordReducerResult(ctx, intent, Result{}, duration, queueWait, "ack_failed", workerID, heartbeatErr)
		return Result{}, false, fmt.Errorf("heartbeat reducer work: %w", heartbeatErr)
	}

	s.recordReducerResult(ctx, intent, result, duration, queueWait, status, workerID, nil)
	return result, true, nil
}
