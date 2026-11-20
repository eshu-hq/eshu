// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// runBatchAckLoop collects worker results and batches acks. It is shared by
// the rendezvous and prefetch claim paths: both hand each executed item to
// the acker exactly once, so the flush, retry, and shutdown-drain ownership
// proof lives in one place.
func (s Service) runBatchAckLoop(
	ctx context.Context,
	batchSink BatchWorkSink,
	batchSize int,
	ackCh <-chan batchAckItem,
	appendErr func(error),
) <-chan struct{} {
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
	return ackDone
}
