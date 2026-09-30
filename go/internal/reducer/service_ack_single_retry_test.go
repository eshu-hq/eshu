// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// transientSingleAckSink fails the first failFirst Ack calls with err, then
// records the ack. It deliberately does not implement BatchWorkSink, so a
// Service with Workers > 1 routes through runPerItemConcurrent and a Service
// with Workers <= 1 through runSequential: both reach ackReducerWork.
type transientSingleAckSink struct {
	mu        sync.Mutex
	err       error
	failFirst int
	ackCalls  int
	failCalls int
	acked     []string
	onAck     func()
}

func (s *transientSingleAckSink) Ack(_ context.Context, intent Intent, _ Result) error {
	s.mu.Lock()
	s.ackCalls++
	call := s.ackCalls
	onAck := s.onAck
	if call > s.failFirst {
		s.acked = append(s.acked, intent.IntentID)
	}
	s.mu.Unlock()
	if onAck != nil {
		onAck()
	}
	if call <= s.failFirst {
		return fmt.Errorf("ack reducer work row: %w", s.err)
	}
	return nil
}

func (s *transientSingleAckSink) Fail(context.Context, Intent, error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCalls++
	return nil
}

// singleAckRun is what runSingleAck observed: Run's error, the captured logs
// and the per-status reducer execution counts.
type singleAckRun struct {
	err    error
	logs   string
	counts map[string]int64
}

// runSingleAck drives a Service whose sink cannot batch-ack, so every ack goes
// through ackReducerWork.
func runSingleAck(
	ctx context.Context,
	t *testing.T,
	sink WorkSink,
	workers int,
	intents []Intent,
) singleAckRun {
	t.Helper()
	instruments, reader := batchTelemetry(t)
	var logs bytes.Buffer
	svc := Service{
		PollInterval: time.Millisecond,
		WorkSource:   &stubReducerWorkSource{intents: intents},
		Executor: &stubReducerExecutor{result: Result{
			Domain: DomainRepoDependency,
			Status: ResultStatusSucceeded,
		}},
		WorkSink:     sink,
		Workers:      workers,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
		Logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
		Instruments:  instruments,
		ackRetryBase: time.Millisecond,
	}
	err := svc.Run(ctx)
	counts, _ := batchMetricStatusCounts(t, reader)
	return singleAckRun{err: err, logs: logs.String(), counts: counts}
}

func timeoutCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestServiceSingleAckRetriesTransientFailure pins #7444 item 1: a 40P01 or
// 40001 on the single-item Ack is a normal retryable database outcome. Before
// the fix it ended the run on both the sequential (Workers <= 1) and the
// per-item concurrent path, the same run-kill #7267 fixed for AckBatch.
func TestServiceSingleAckRetriesTransientFailure(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"40P01", "40001"} {
		for _, workers := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/workers=%d", code, workers), func(t *testing.T) {
				t.Parallel()

				sink := &transientSingleAckSink{err: &pgconn.PgError{Code: code}, failFirst: 1}
				intents := makeTestIntents(3)

				run := runSingleAck(timeoutCtx(t), t, sink, workers, intents)
				if run.err != nil {
					t.Fatalf("Run() error = %v, want nil after a transient single ack failure", run.err)
				}
				sink.mu.Lock()
				acked, calls, failed := len(sink.acked), sink.ackCalls, sink.failCalls
				sink.mu.Unlock()
				if acked != len(intents) {
					t.Fatalf("acked intents = %d, want %d after the retried ack", acked, len(intents))
				}
				if want := len(intents) + 1; calls != want {
					t.Fatalf("Ack calls = %d, want %d (one failure, one retry, then the rest)", calls, want)
				}
				if failed != 0 {
					t.Fatalf("Fail calls = %d, want 0: a retried ack must not dead-letter finished work", failed)
				}
				if run.counts["succeeded"] != int64(len(intents)) || len(run.counts) != 1 {
					t.Fatalf("reducer execution statuses = %v, want only succeeded:%d", run.counts, len(intents))
				}
				if !strings.Contains(run.logs, `"failure_class":"ack_transient_retry"`) {
					t.Fatalf("logs omit the ack_transient_retry warning an operator would search for: %s", run.logs)
				}
			})
		}
	}
}

// TestServiceSingleAckAbandonsExhaustedTransientToLeaseExpiry keeps the run
// draining when the single-item ack keeps failing transiently and gives the
// abandonment its own counted outcome (#7444 item 2) instead of the
// ack_outcome_unknown status that shutdown cancellation also uses.
func TestServiceSingleAckAbandonsExhaustedTransientToLeaseExpiry(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{1, 2} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			t.Parallel()

			sink := &transientSingleAckSink{err: &pgconn.PgError{Code: "40P01"}, failFirst: 1 << 30}
			intents := makeTestIntents(2)

			run := runSingleAck(timeoutCtx(t), t, sink, workers, intents)
			if run.err != nil {
				t.Fatalf("Run() error = %v, want nil: exhausted transient acks must not cancel the run", run.err)
			}
			sink.mu.Lock()
			calls, failed := sink.ackCalls, sink.failCalls
			sink.mu.Unlock()
			if want := len(intents) * ackRetryAttempts; calls != want {
				t.Fatalf("Ack calls = %d, want %d (the bounded budget for each intent)", calls, want)
			}
			if failed != 0 {
				t.Fatalf("Fail calls = %d, want 0: abandoned work is reclaimed at lease expiry, not failed", failed)
			}
			if got := run.counts["ack_abandoned_to_lease_expiry"]; got != int64(len(intents)) || len(run.counts) != 1 {
				t.Fatalf("reducer execution statuses = %v, want only ack_abandoned_to_lease_expiry:%d", run.counts, len(intents))
			}
			for _, want := range []string{
				`"status":"ack_abandoned_to_lease_expiry"`,
				`"failure_class":"ack_abandoned_to_lease_expiry"`,
			} {
				if !strings.Contains(run.logs, want) {
					t.Fatalf("logs missing %s in %s", want, run.logs)
				}
			}
			if strings.Contains(run.logs, `"msg":"reducer execution succeeded"`) {
				t.Fatalf("an abandoned ack was logged as a success: %s", run.logs)
			}
		})
	}
}

// TestServiceSingleAckStillFailsOnNonTransientError guards the fix's reach:
// only serialization failures are retried, so any other ack error still ends
// the run on the first attempt with the ack_failed status.
func TestServiceSingleAckStillFailsOnNonTransientError(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{1, 2} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			t.Parallel()

			sink := &transientSingleAckSink{err: errors.New("connection refused"), failFirst: 1 << 30}

			run := runSingleAck(timeoutCtx(t), t, sink, workers, makeTestIntents(1))
			if run.err == nil || !strings.Contains(run.err.Error(), "ack reducer work") {
				t.Fatalf("Run() error = %v, want the non-transient ack error", run.err)
			}
			sink.mu.Lock()
			calls := sink.ackCalls
			sink.mu.Unlock()
			if calls != 1 {
				t.Fatalf("Ack calls = %d, want 1 (no retry for a non-transient error)", calls)
			}
			if run.counts["ack_failed"] != 1 || len(run.counts) != 1 {
				t.Fatalf("reducer execution statuses = %v, want only ack_failed:1", run.counts)
			}
		})
	}
}

// TestServiceSingleAckShutdownDuringBackoffIsNotAbandonment proves the new
// status does not swallow shutdown: cancelling while the retry waits is an
// unknown ack outcome, exactly as before, and is not counted as abandoned.
func TestServiceSingleAckShutdownDuringBackoffIsNotAbandonment(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(timeoutCtx(t))
	sink := &transientSingleAckSink{
		err:       &pgconn.PgError{Code: "40P01"},
		failFirst: 1 << 30,
		onAck:     cancel,
	}

	run := runSingleAck(ctx, t, sink, 1, makeTestIntents(1))
	if run.err != nil {
		t.Fatalf("Run() error = %v, want nil on shutdown", run.err)
	}
	sink.mu.Lock()
	calls := sink.ackCalls
	sink.mu.Unlock()
	if calls != 1 {
		t.Fatalf("Ack calls = %d, want 1 (shutdown interrupts the first backoff)", calls)
	}
	if run.counts["ack_outcome_unknown"] != 1 || len(run.counts) != 1 {
		t.Fatalf("reducer execution statuses = %v, want only ack_outcome_unknown:1", run.counts)
	}
	if strings.Contains(run.logs, `"failure_class":"ack_abandoned_to_lease_expiry"`) {
		t.Fatalf("shutdown was logged as abandonment: %s", run.logs)
	}
}

// TestServiceSingleAckNonTransientErrorAtCancellationStillFailsRun pins the
// edge of the shutdown branch: only a transient-ack retry that shutdown cut
// short is treated as shutdown. A non-transient ack error that lands while the
// context is being cancelled is a real ack failure and still ends the run, so
// loosening the branch to "the context is done" would fail here.
func TestServiceSingleAckNonTransientErrorAtCancellationStillFailsRun(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(timeoutCtx(t))
	sink := &transientSingleAckSink{
		err:       errors.New("connection reset"),
		failFirst: 1 << 30,
		onAck:     cancel,
	}

	run := runSingleAck(ctx, t, sink, 1, makeTestIntents(1))
	if run.err == nil || !strings.Contains(run.err.Error(), "ack reducer work") {
		t.Fatalf("Run() error = %v, want the non-transient ack error", run.err)
	}
	if run.counts["ack_failed"] != 1 || len(run.counts) != 1 {
		t.Fatalf("reducer execution statuses = %v, want only ack_failed:1", run.counts)
	}
}
