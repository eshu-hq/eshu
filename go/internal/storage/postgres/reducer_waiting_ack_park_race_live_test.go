// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11228 Ack/park interleave proofs: a generation_active wait that parks between the projector Ack's in-tx wake and its commit is reopened without the audit or backstop, in both orders (park-first, ack-first) and the held window; disposable database per test; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ackParkRaceCollectorKind maps to a producer domain
// (container_image_identity), so the Ack runs one producer-terminal wake
// statement after its generation_active wake. The proof holds the Ack there.
const ackParkRaceCollectorKind = "oci_registry"

// ackParkRaceLeaseOwner is the projector lease owner the proof's Ack uses.
const ackParkRaceLeaseOwner = "ack-park-race-projector"

// ackParkRaceFixture is one seeded scope with a pending generation, a
// claimed reducer waiter on it, and the projector work item that activates it.
type ackParkRaceFixture struct {
	base      *sql.DB
	targetDSN string
	clock     time.Time
	scopeID   string
	genID     string
	waiterID  string
	waiterQ   ReducerQueue
	waiter    reducercontract.Intent
}

func newAckParkRaceFixture(t *testing.T, ctx context.Context, scopeID string) *ackParkRaceFixture {
	t.Helper()
	base, targetDSN := openWaitingDB(t, ctx)
	f := &ackParkRaceFixture{
		base: base, targetDSN: targetDSN,
		clock:    time.Now().UTC().Truncate(time.Second),
		scopeID:  scopeID,
		genID:    scopeID + "-g1",
		waiterID: scopeID + "-waiter",
	}
	seedWaitingScope(t, ctx, base, scopeID, scopeID+"-g0", f.genID, f.clock)
	addOwnGateItem(t, ctx, base, f.waiterID, scopeID, f.genID, reducer.DomainDeployableUnitCorrelation, f.clock)
	waiterDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	f.waiterQ = newOwnGateQueue(waiterDB, &f.clock, reducer.DomainDeployableUnitCorrelation)
	intent, ok, err := f.waiterQ.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim waiter: ok=%v err=%v", ok, err)
	}
	if intent.IntentID != f.waiterID {
		t.Fatalf("claimed %q, want %q", intent.IntentID, f.waiterID)
	}
	f.waiter = intent
	if err := seedProjectorRunningErr(ctx, base, scopeID, f.genID, ackParkRaceLeaseOwner); err != nil {
		t.Fatal(err)
	}
	return f
}

// ack runs the production projector Ack on its own connection with the
// fixture's frozen clock and the longest permitted Ack lock timeout. It
// reports through its return value, so it is safe off the test goroutine.
func (f *ackParkRaceFixture) ack(ctx context.Context, ackDB *sql.DB) error {
	queue := NewProjectorQueue(SQLDB{DB: ackDB}, ackParkRaceLeaseOwner, 10*time.Minute)
	at := f.clock
	queue.Now = func() time.Time { return at }
	queue.AckScopeLockTimeout = maxProjectorAckLockTimeout
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       f.scopeID,
			CollectorKind: scope.CollectorKind(ackParkRaceCollectorKind),
		},
		Generation:   scope.ScopeGeneration{GenerationID: f.genID},
		AttemptCount: 1,
	}
	return queue.Ack(ctx, work, runtime.Result{})
}

// park runs the production reducer Fail that parks the waiter on its
// generation_active wait.
func (f *ackParkRaceFixture) park(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := f.waiterQ.Fail(ctx, f.waiter, notYetActive(f.scopeID, f.genID)); err != nil {
		t.Fatalf("park waiter: %v", err)
	}
}

// requireWoken asserts the waiter left the set-aside without the audit or the
// backstop: no wait row, still retrying in its non-counting class, claimable
// at the wake stamp (not the 10-minute backstop), attempt budget untouched.
func (f *ackParkRaceFixture) requireWoken(t *testing.T, ctx context.Context) {
	t.Helper()
	if got := f.generationStatus(t, ctx); got != "active" {
		t.Fatalf("generation status = %q, want active (the Ack committed)", got)
	}
	got := readWaitingRow(t, ctx, f.base, f.waiterID)
	if got.waitRows != 0 {
		t.Fatalf("waiter %s still parked (wait rows = %d, visible_at = %v, backstop = %v): the Ack's wake was lost and only the audit or the backstop can reopen it (#11228)",
			f.waiterID, got.waitRows, got.visibleAt.Time, f.clock.Add(WaitingBackstopDelay))
	}
	if got.status != "retrying" || !got.visibleAt.Valid || got.visibleAt.Time.After(f.clock) {
		t.Fatalf("waiter %s = status %q visible_at %v, want retrying and claimable at %v",
			f.waiterID, got.status, got.visibleAt.Time, f.clock)
	}
	if got.attempts != 1 {
		t.Fatalf("waiter %s attempt_count = %d, want 1 (the wait never counts)", f.waiterID, got.attempts)
	}
}

func (f *ackParkRaceFixture) generationStatus(t *testing.T, ctx context.Context) string {
	t.Helper()
	var status string
	if err := f.base.QueryRowContext(ctx,
		`SELECT status FROM scope_generations WHERE scope_id = $1 AND generation_id = $2`,
		f.scopeID, f.genID).Scan(&status); err != nil {
		t.Fatalf("read generation: %v", err)
	}
	return status
}

// holdAckAfterGenerationWake plants a committed producer-terminal wait row
// on the Ack's producer key and row-locks it on its own connection, so the
// Ack's producer-terminal wake (the statement after its generation_active
// wake, before its commit) blocks. release ends the hold.
func holdAckAfterGenerationWake(t *testing.T, ctx context.Context, f *ackParkRaceFixture) (blockerPID int, release func()) {
	t.Helper()
	holdID := f.scopeID + "-hold"
	addOwnGateItem(t, ctx, f.base, holdID, f.scopeID, f.genID, reducer.DomainContainerImageIdentity, f.clock)
	// Retrying, as a parked row is: the wake joins its waiting CTE to
	// retrying queue rows, and with no retrying row on the key the planner
	// never scans the CTE, so its FOR UPDATE would take no lock.
	if _, err := f.base.ExecContext(ctx, `
UPDATE fact_work_items SET status = 'retrying', visible_at = $2 WHERE work_item_id = $1`,
		holdID, f.clock.Add(WaitingBackstopDelay)); err != nil {
		t.Fatalf("park hold row: %v", err)
	}
	if _, err := f.base.ExecContext(ctx, `
INSERT INTO reducer_waiting_items (work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id,
    domain, failure_class, first_deferred_at, updated_at)
VALUES ($1, 'producer_domain_terminal', $2::jsonb, $3, $4, 'container_image_identity', 'hold', $5, $5)`,
		holdID, waitingProducerDomainKey(string(reducer.DomainContainerImageIdentity)),
		f.scopeID, f.genID, f.clock); err != nil {
		t.Fatalf("plant hold wait row: %v", err)
	}
	holdDB := openReducerFairnessClaimerDB(t, ctx, f.targetDSN)
	tx, err := holdDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin hold tx: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("hold pid: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT 1 FROM reducer_waiting_items WHERE work_item_id = $1 FOR UPDATE`, holdID); err != nil {
		t.Fatalf("lock hold wait row: %v", err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			_ = tx.Rollback()
		}
	}
	t.Cleanup(release)
	return blockerPID, release
}

// waitBlockedBy polls until some backend waits on blockerPID's locks. The
// bound stays under the Ack's 4-second lock_timeout.
func waitBlockedBy(t *testing.T, ctx context.Context, db *sql.DB, blockerPID int, ackDone <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var blocked int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY (pg_blocking_pids(pid))`,
			blockerPID).Scan(&blocked); err != nil {
			t.Fatalf("poll blocked backends: %v", err)
		}
		if blocked > 0 {
			return
		}
		select {
		case err := <-ackDone:
			t.Fatalf("Ack finished before it reached the hold: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("the Ack never blocked on the hold after its generation_active wake")
}

// TestWaitingAckParkInterleaveLive pins the #11228 lost wake: the reducer
// parks its generation_active wait after the projector Ack's in-tx wake ran
// and before the Ack commits. The in-tx wake cannot see the wait row, and the
// park's re-check cannot see the uncommitted activation. The Ack's
// post-commit re-wake must reopen the waiter; without it the row sits out
// the audit (5 minutes) and the backstop (10 minutes).
func TestWaitingAckParkInterleaveLive(t *testing.T) {
	ctx := context.Background()
	f := newAckParkRaceFixture(t, ctx, "ack-park-held")
	blockerPID, release := holdAckAfterGenerationWake(t, ctx, f)

	ackDB := openReducerFairnessClaimerDB(t, ctx, f.targetDSN)
	ackDone := make(chan error, 1)
	go func() { ackDone <- f.ack(ctx, ackDB) }()
	waitBlockedBy(t, ctx, f.base, blockerPID, ackDone)

	// The Ack ran its generation_active wake and holds its transaction open.
	if got := f.generationStatus(t, ctx); got != "pending" {
		t.Fatalf("generation status inside the window = %q, want pending (Ack uncommitted)", got)
	}

	// The park runs entirely inside the window: insert, re-check, no wake.
	f.park(t, ctx)
	if got := readWaitingRow(t, ctx, f.base, f.waiterID); got.waitRows != 1 {
		t.Fatalf("waiter wait rows inside the window = %d, want 1 (parked)", got.waitRows)
	}

	release()
	select {
	case err := <-ackDone:
		if err != nil {
			t.Fatalf("Ack: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ack never committed after the hold released")
	}
	f.requireWoken(t, ctx)
}

// TestWaitingAckParkOrdersLive covers the two unheld orders with the same
// production paths: the park commits before the Ack starts (the in-tx wake
// reopens it), and the Ack commits before the park (the park's re-check
// reopens it at once). Each ends woken with no wait row.
func TestWaitingAckParkOrdersLive(t *testing.T) {
	ctx := context.Background()
	t.Run("park_first", func(t *testing.T) {
		f := newAckParkRaceFixture(t, ctx, "ack-park-first")
		f.park(t, ctx)
		if got := readWaitingRow(t, ctx, f.base, f.waiterID); got.waitRows != 1 {
			t.Fatalf("wait rows after park = %d, want 1", got.waitRows)
		}
		if err := f.ack(ctx, openReducerFairnessClaimerDB(t, ctx, f.targetDSN)); err != nil {
			t.Fatalf("Ack: %v", err)
		}
		f.requireWoken(t, ctx)
	})
	t.Run("ack_first", func(t *testing.T) {
		f := newAckParkRaceFixture(t, ctx, "ack-park-ackfirst")
		if err := f.ack(ctx, openReducerFairnessClaimerDB(t, ctx, f.targetDSN)); err != nil {
			t.Fatalf("Ack: %v", err)
		}
		f.park(t, ctx)
		f.requireWoken(t, ctx)
	})
}
