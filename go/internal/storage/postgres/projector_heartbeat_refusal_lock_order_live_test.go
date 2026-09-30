// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// pauseRefusalAfterWorkLockSQL installs a BEFORE UPDATE trigger that sleeps
// while the delta-baseline refusal holds the work row's tuple lock, before it
// reaches the generation row. It fires only for the refusal's failure class.
const pauseRefusalAfterWorkLockSQL = `
CREATE FUNCTION pause_refusal_after_work_lock() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_sleep(1.5);
    RETURN NEW;
END $$;
CREATE TRIGGER pause_refusal_after_work_lock
    BEFORE UPDATE ON fact_work_items
    FOR EACH ROW
    WHEN (NEW.failure_class = 'projector_delta_baseline_mismatch')
    EXECUTE FUNCTION pause_refusal_after_work_lock();
`

// TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal pins the #7389 lock
// order. The delta-baseline refusal locks the work row, then the generation
// row, and takes no scope row. A heartbeat supersede that locked the
// generation first and then waited on the work row formed a cycle with it
// (40P01), and a heartbeat error cancels the whole projector worker pool. The
// heartbeat must take every lock with SKIP LOCKED in scope, work, generation
// order, so it skips the held work row instead of waiting. The trigger pauses
// the refusal right after it takes the work row, and the heartbeat runs in
// that window.
func TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal(t *testing.T) {
	dsn := supersessionProofDSN(t)
	control := heartbeatProofDB(t, writeMarkerInterleaveSeed+pauseRefusalAfterWorkLockSQL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	refusalDB := openSessionOnProofSchema(ctx, t, control, dsn)
	heartbeatDB := openSessionOnProofSchema(ctx, t, control, dsn)
	var refusalPID int
	if err := refusalDB.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&refusalPID); err != nil {
		t.Fatalf("refusal pid: %v", err)
	}

	work := heartbeatProofWork("gen-il")
	refusal := projector.DeltaBaselineRefusal{
		Phase:   projector.DeltaBaselinePhasePreflight,
		Outcome: projector.DeltaBaselineRefusedActiveDiffers,
		State: projector.DeltaBaselineState{
			TargetFound: true, IsDelta: true, BaselineCommitSHA: "a",
			ActiveGenerationID: "gen-pub", ActiveCommitSHA: "b",
		},
	}
	refused := make(chan error, 1)
	go func() {
		refused <- NewProjectorQueue(SQLDB{DB: refusalDB}, "proof-worker", time.Minute).RefuseDeltaBaseline(ctx, work, refusal)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var sleeping bool
		if err := control.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
WHERE pid = $1 AND wait_event = 'PgSleep')`, refusalPID).Scan(&sleeping); err != nil {
			t.Fatalf("watch refusal: %v", err)
		}
		if sleeping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the refusal never paused holding the work row")
		}
		time.Sleep(10 * time.Millisecond)
	}

	heartbeatErr := NewProjectorQueue(SQLDB{DB: heartbeatDB}, "proof-worker", time.Minute).
		supersedeRunningWork(ctx, work, time.Now().UTC())
	refusalErr := <-refused

	var pgErr *pgconn.PgError
	for name, err := range map[string]error{"heartbeat supersede": heartbeatErr, "refusal": refusalErr} {
		if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("%s = %v: the heartbeat and the refusal deadlocked", name, err)
		}
	}
	if heartbeatErr != nil {
		t.Fatalf("heartbeat supersede while the refusal holds the work row = %v, want a no-op (SKIP LOCKED)", heartbeatErr)
	}
	if !errors.Is(refusalErr, failure.ErrWorkSuperseded) {
		t.Fatalf("refusal = %v, want ErrWorkSuperseded (it marked the work row)", refusalErr)
	}
	if got := readWriteMarkerState(ctx, t, control); got != "superseded,false,superseded" {
		t.Fatalf("state = %s, want superseded,false,superseded (the refusal's outcome)", got)
	}
}
