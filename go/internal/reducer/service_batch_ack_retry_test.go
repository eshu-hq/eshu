// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// transientAckSink fails the first failFirst AckBatch calls with err, then
// delegates to the embedded recording sink.
type transientAckSink struct {
	fakeBatchWorkSink
	err        error
	failFirst  int64
	ackBatches atomic.Int64
}

func (s *transientAckSink) AckBatch(ctx context.Context, intents []Intent, results []Result) error {
	if s.ackBatches.Add(1) <= s.failFirst {
		return fmt.Errorf("batch ack reducer work (%d items): %w", len(intents), s.err)
	}
	return s.fakeBatchWorkSink.AckBatch(ctx, intents, results)
}

func runAckRetryService(t *testing.T, sink WorkSink, intents []Intent) error {
	t.Helper()
	svc := Service{
		PollInterval:   10 * time.Millisecond,
		WorkSource:     &fakeBatchWorkSource{intents: intents},
		Executor:       &countingExecutor{},
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: 8,
		ackRetryBase:   time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return svc.runMainLoop(ctx)
}

// TestServiceRunBatchRetriesTransientAckFailure pins #7267: a 40P01 deadlock
// or 40001 serialization failure on AckBatch is a normal, retryable database
// outcome. It used to reach appendErr, cancel the run and end the reducer.
func TestServiceRunBatchRetriesTransientAckFailure(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"40P01", "40001"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()

			sink := &transientAckSink{err: &pgconn.PgError{Code: code}, failFirst: 1}
			intents := makeTestIntents(3)

			if err := runAckRetryService(t, sink, intents); err != nil {
				t.Fatalf("runMainLoop() error = %v, want nil after a transient ack failure", err)
			}

			sink.mu.Lock()
			acked := len(sink.ackIDs)
			sink.mu.Unlock()
			if acked != len(intents) {
				t.Fatalf("acked intents = %d, want %d after the retried ack", acked, len(intents))
			}
			if got := sink.ackBatches.Load(); got < 2 {
				t.Fatalf("AckBatch calls = %d, want >= 2 (failure then retry)", got)
			}
		})
	}
}

// TestServiceRunBatchAbandonsExhaustedTransientAckToLeaseExpiry keeps the run
// alive when the ack keeps failing transiently: the claims are left to expire
// and be reclaimed instead of cancelling every in-flight worker.
func TestServiceRunBatchAbandonsExhaustedTransientAckToLeaseExpiry(t *testing.T) {
	t.Parallel()

	sink := &transientAckSink{err: &pgconn.PgError{Code: "40P01"}, failFirst: 1 << 30}

	if err := runAckRetryService(t, sink, makeTestIntents(2)); err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil: exhausted transient acks must not cancel the run", err)
	}
	if got := sink.ackBatches.Load(); got != int64(ackRetryAttempts) {
		t.Fatalf("AckBatch calls = %d, want the bounded budget %d", got, ackRetryAttempts)
	}
}

// TestServiceRunBatchStillFailsOnNonTransientAckError guards the fix's reach:
// only serialization failures are retried; any other ack error still ends the
// run as before.
func TestServiceRunBatchStillFailsOnNonTransientAckError(t *testing.T) {
	t.Parallel()

	sink := &transientAckSink{err: errors.New("connection refused"), failFirst: 1 << 30}

	if err := runAckRetryService(t, sink, makeTestIntents(1)); err == nil {
		t.Fatal("runMainLoop() error = nil, want the non-transient ack error")
	}
	if got := sink.ackBatches.Load(); got != 1 {
		t.Fatalf("AckBatch calls = %d, want 1 (no retry for a non-transient error)", got)
	}
}
