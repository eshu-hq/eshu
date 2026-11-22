// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestRetryTransientAckCancelledContextNeverRetries pins the shutdown ordering
// of the transient-ack retry loop (#11194). When the context is already
// cancelled and the backoff timer has already fired, both select arms are
// ready and Go picks one at random, so an unchecked loop re-ran ack after
// shutdown about half the time. A 1 ns backoff guarantees the timer is ready
// by the time the loop selects, so every iteration exercises the both-ready
// case; with a random pick the chance of 256 clean runs against the old loop
// is 2^-256.
func TestRetryTransientAckCancelledContextNeverRetries(t *testing.T) {
	t.Parallel()

	svc := Service{ackRetryBase: time.Nanosecond}
	msgs := ackRetryMessages{retry: "retry", abandoned: "abandoned"}

	for i := 0; i < 256; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := svc.retryTransientAck(ctx, 1, msgs, func() error {
			calls++
			cancel() // shutdown lands while the first ack is in flight
			return &pgconn.PgError{Code: "40P01"}
		})
		if calls != 1 {
			t.Fatalf("run %d: ack calls = %d, want 1 (a cancelled context must not retry)", i, calls)
		}
		if !errors.Is(err, errAckRetryInterrupted) || !errors.Is(err, context.Canceled) {
			t.Fatalf("run %d: err = %v, want errAckRetryInterrupted wrapping context.Canceled", i, err)
		}
	}
}
