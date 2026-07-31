// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ackLatencyFakeDB implements ExecQueryer + Beginner + Transaction with
// zero-cost operations, isolating the measurement below to the hook's own
// added work rather than fake-DB overhead.
type ackLatencyFakeDB struct{}

func (ackLatencyFakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return driverResult{}, nil
}

func (ackLatencyFakeDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("query not expected in this benchmark-style test")
}

func (f ackLatencyFakeDB) Begin(context.Context) (Transaction, error) {
	return ackLatencyFakeTx{}, nil
}

type ackLatencyFakeTx struct{ ackLatencyFakeDB }

func (ackLatencyFakeTx) Commit() error   { return nil }
func (ackLatencyFakeTx) Rollback() error { return nil }

// sleepingConfigStateDriftTrigger simulates reducerAdmissionWriter.Enqueue's
// real deferral loop (cmd/ingester/reducer_admission.go): when the shared
// reducer queue is over its high-water mark or under graph-write-timeout
// pressure, that writer blocks in a for { ...; sleep(ctx, PollInterval) }
// loop before the SAME call this hook makes (t.Queue.Enqueue) returns.
// deferrals controls how many PollInterval-length sleeps happen before the
// call succeeds -- 0 reproduces the "admission not deferring" common case.
type sleepingConfigStateDriftTrigger struct {
	deferrals    int
	pollInterval time.Duration
}

func (s sleepingConfigStateDriftTrigger) TriggerConfigStateDrift(ctx context.Context, _, _ string) error {
	for i := 0; i < s.deferrals; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.pollInterval):
		}
	}
	return nil
}

// measureAck runs Ack n times for a state_snapshot:* scope through the given
// trigger and returns the total wall-clock duration.
func measureAck(t *testing.T, trigger ConfigStateDriftTrigger, n int) time.Duration {
	t.Helper()
	queue := ProjectorQueue{
		db:                      ackLatencyFakeDB{},
		LeaseOwner:              "latency-test",
		LeaseDuration:           time.Minute,
		ConfigStateDriftTrigger: trigger,
	}
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "state_snapshot:s3:hash-1"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-1"},
	}

	start := time.Now()
	for i := 0; i < n; i++ {
		if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
			t.Fatalf("Ack() error = %v, want nil", err)
		}
	}
	return time.Since(start)
}

// TestProjectorQueueAckConfigStateDriftTriggerLatencyUnderAdmissionBackpressure
// is the issue #5593 P2-1 Performance Evidence measurement: proves, with
// real numbers on the exact code path, that ConfigStateDriftTrigger couples
// Ack's return latency to the shared reducer admission queue's deferral
// state for state_snapshot:* scopes ONLY, and quantifies the coupling as
// deferrals * pollInterval (matching reducerAdmissionWriter.wait's actual
// sleep-per-deferral shape) rather than asserting it.
//
// Three shapes on the SAME Ack primitive:
//   - baseline: ConfigStateDriftTrigger unwired (pre-#5593 behavior for every
//     scope kind, and post-#5593 behavior for bootstrap-index's ProjectorQueue,
//     which never wires the trigger).
//   - no-deferral: the trigger wired, admission not deferring (the common
//     case in a healthy steady-state ingester) -- measures the hook's fixed
//     added cost (prefix check + one Enqueue call) with none of the
//     admission-writer's own sleep loop engaged.
//   - N-deferral: the trigger wired, admission deferring N times before
//     succeeding -- measures the worst-case coupling.
func TestProjectorQueueAckConfigStateDriftTriggerLatencyUnderAdmissionBackpressure(t *testing.T) {
	const iterations = 20
	const pollInterval = 20 * time.Millisecond // scaled down from a real deployment's typical multi-second PollInterval; the relationship is linear in deferrals, so this scaling does not change the conclusion.

	baseline := measureAck(t, nil, iterations)
	noDeferral := measureAck(t, sleepingConfigStateDriftTrigger{deferrals: 0, pollInterval: pollInterval}, iterations)
	oneDeferral := measureAck(t, sleepingConfigStateDriftTrigger{deferrals: 1, pollInterval: pollInterval}, iterations)
	threeDeferrals := measureAck(t, sleepingConfigStateDriftTrigger{deferrals: 3, pollInterval: pollInterval}, iterations)

	t.Logf(
		"Ack latency over %d iterations: baseline(no trigger)=%v (%v/op), no-deferral=%v (%v/op), 1-deferral=%v (%v/op), 3-deferrals=%v (%v/op), pollInterval=%v",
		iterations,
		baseline, baseline/iterations,
		noDeferral, noDeferral/iterations,
		oneDeferral, oneDeferral/iterations,
		threeDeferrals, threeDeferrals/iterations,
		pollInterval,
	)

	// The no-deferral case must stay a small, bounded fixed cost -- not
	// coupled to pollInterval at all. A generous 5x baseline-or-10ms ceiling
	// (whichever is larger) rules out an accidental sleep/lock on the
	// no-op-fast path while tolerating normal scheduler jitter in CI.
	noDeferralCeiling := 10 * time.Millisecond * iterations
	if baseline*5 > noDeferralCeiling {
		noDeferralCeiling = baseline * 5
	}
	if noDeferral > noDeferralCeiling {
		t.Fatalf("no-deferral Ack latency = %v, want <= %v (fixed hook overhead must not scale with pollInterval)", noDeferral, noDeferralCeiling)
	}

	// The 1-deferral and 3-deferral cases must each be AT LEAST their
	// deferral count's worth of pollInterval sleeps -- proving Ack's return
	// really is blocked on the simulated admission wait loop, not merely
	// scheduled after it asynchronously.
	if want := time.Duration(iterations) * pollInterval; oneDeferral < want {
		t.Fatalf("1-deferral Ack latency = %v, want >= %v (%d iterations * 1 deferral * pollInterval)", oneDeferral, want, iterations)
	}
	if want := time.Duration(iterations) * 3 * pollInterval; threeDeferrals < want {
		t.Fatalf("3-deferral Ack latency = %v, want >= %v (%d iterations * 3 deferrals * pollInterval)", threeDeferrals, want, iterations)
	}
	// And the 3-deferral case must be measurably larger than the 1-deferral
	// case -- proving the relationship is linear in deferral count, not a
	// fixed one-time cost regardless of how long admission defers.
	if threeDeferrals <= oneDeferral {
		t.Fatalf("3-deferral Ack latency (%v) must exceed 1-deferral Ack latency (%v) -- the coupling must scale with deferral count", threeDeferrals, oneDeferral)
	}
}
