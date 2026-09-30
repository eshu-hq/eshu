// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// cancelingTransientAckSink always fails AckBatch with a transient error and
// runs onAck first, so a test can cancel the run while the retry is about to
// back off.
type cancelingTransientAckSink struct {
	transientAckSink
	onAck func()
}

func (s *cancelingTransientAckSink) AckBatch(ctx context.Context, intents []Intent, results []Result) error {
	if s.onAck != nil {
		s.onAck()
	}
	return s.transientAckSink.AckBatch(ctx, intents, results)
}

func runBatchAckStatuses(
	ctx context.Context,
	t *testing.T,
	sink WorkSink,
	intents []Intent,
) (map[string]int64, string, error) {
	t.Helper()
	instruments, reader := batchTelemetry(t)
	var logs bytes.Buffer
	svc := Service{
		PollInterval:   time.Millisecond,
		WorkSource:     &fakeBatchWorkSource{intents: intents},
		Executor:       &countingExecutor{},
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: len(intents),
		Wait:           func(context.Context, time.Duration) error { return context.Canceled },
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		Instruments:    instruments,
		ackRetryBase:   time.Millisecond,
	}
	err := svc.runMainLoop(ctx)
	counts, _ := batchMetricStatusCounts(t, reader)
	return counts, logs.String(), err
}

// TestServiceBatchAckAbandonedHasDistinctStatus pins #7444 item 2 on the batch
// path: acks abandoned to lease expiry are counted under their own execution
// status rather than ack_outcome_unknown, which shutdown cancellation shares.
func TestServiceBatchAckAbandonedHasDistinctStatus(t *testing.T) {
	t.Parallel()

	sink := &transientAckSink{err: &pgconn.PgError{Code: "40P01"}, failFirst: 1 << 30}
	intents := makeTestIntents(2)

	counts, logs, err := runBatchAckStatuses(timeoutCtx(t), t, sink, intents)
	if err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil: exhausted transient acks must not cancel the run", err)
	}
	if got := counts["ack_abandoned_to_lease_expiry"]; got != int64(len(intents)) || len(counts) != 1 {
		t.Fatalf("reducer execution statuses = %v, want only ack_abandoned_to_lease_expiry:%d", counts, len(intents))
	}
	if !strings.Contains(logs, `"status":"ack_abandoned_to_lease_expiry"`) {
		t.Fatalf("logs omit the abandoned status: %s", logs)
	}
	if strings.Contains(logs, `"msg":"reducer execution succeeded"`) {
		t.Fatalf("an abandoned batch ack was logged as a success: %s", logs)
	}
}

// TestServiceBatchAckShutdownDuringBackoffIsNotAbandonment keeps shutdown on
// the ack_outcome_unknown status: cancelling while the retry waits is not an
// abandonment.
func TestServiceBatchAckShutdownDuringBackoffIsNotAbandonment(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(timeoutCtx(t))
	sink := &cancelingTransientAckSink{
		transientAckSink: transientAckSink{err: &pgconn.PgError{Code: "40P01"}, failFirst: 1 << 30},
		onAck:            cancel,
	}
	intents := makeTestIntents(2)

	counts, logs, err := runBatchAckStatuses(ctx, t, sink, intents)
	if err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil on shutdown", err)
	}
	if counts["ack_outcome_unknown"] != int64(len(intents)) || len(counts) != 1 {
		t.Fatalf("reducer execution statuses = %v, want only ack_outcome_unknown:%d", counts, len(intents))
	}
	if strings.Contains(logs, `"status":"ack_abandoned_to_lease_expiry"`) {
		t.Fatalf("shutdown was recorded as abandonment: %s", logs)
	}
}
