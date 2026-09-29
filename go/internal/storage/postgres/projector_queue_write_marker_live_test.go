// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// TestProjectorWriteMarkerHeartbeatOrderingLive is the deterministic half of
// the #7389 exclusion proof, one interleaving per subtest.
func TestProjectorWriteMarkerHeartbeatOrderingLive(t *testing.T) {
	dsn := supersessionProofDSN(t)
	work := heartbeatProofWork("gen-il")

	// A marker held uncommitted owns the generation row: the heartbeat skips
	// it without waiting (a wait would raise 55P03 under the 50 ms
	// lock_timeout) and renews the lease; once the marker commits, the gate
	// keeps the heartbeat from superseding.
	t.Run("marker_uncommitted_then_committed", func(t *testing.T) {
		control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		markerDB := openSessionOnProofSchema(ctx, t, control, dsn)
		heartbeatDB := openSessionOnProofSchema(ctx, t, control, dsn)
		if _, err := heartbeatDB.ExecContext(ctx, "SET lock_timeout = '50ms'"); err != nil {
			t.Fatalf("set heartbeat lock_timeout: %v", err)
		}
		markerTx, err := markerDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin marker tx: %v", err)
		}
		defer func() { _ = markerTx.Rollback() }()
		var marked string
		if err := markerTx.QueryRowContext(ctx, markProjectionWriteStartedQuery,
			"scope-hb", "gen-il", "proof-worker", 1, time.Now().UTC()).Scan(&marked); err != nil {
			t.Fatalf("hold the marker uncommitted: %v", err)
		}
		queue := NewProjectorQueue(SQLDB{DB: heartbeatDB}, "proof-worker", time.Minute)
		if err := queue.Heartbeat(ctx, work); err != nil {
			t.Fatalf("Heartbeat while the marker holds the generation row = %v, want a lease renewal (SKIP LOCKED)", err)
		}
		if got := readWriteMarkerState(ctx, t, control); got != "pending,false,running" {
			t.Fatalf("state with the marker uncommitted = %s, want pending,false,running", got)
		}
		if err := markerTx.Commit(); err != nil {
			t.Fatalf("commit marker: %v", err)
		}
		if err := queue.Heartbeat(ctx, work); err != nil {
			t.Fatalf("Heartbeat after the marker committed = %v, want nil (write started, gate closed)", err)
		}
		if got := readWriteMarkerState(ctx, t, control); got != "pending,true,running" {
			t.Fatalf("state after the marker committed = %s, want pending,true,running", got)
		}
	})

	// A supersede that commits first retires the generation: the marker then
	// matches no row and reports ErrWorkSuperseded, leaving no marker.
	t.Run("supersede_committed_first", func(t *testing.T) {
		control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
		ctx := context.Background()
		queue := NewProjectorQueue(SQLDB{DB: control}, "proof-worker", time.Minute)
		if err := queue.Heartbeat(ctx, work); !errors.Is(err, failure.ErrWorkSuperseded) {
			t.Fatalf("Heartbeat with a newer pending generation = %v, want ErrWorkSuperseded", err)
		}
		if err := queue.MarkProjectionWriteStarted(ctx, work); !errors.Is(err, failure.ErrWorkSuperseded) {
			t.Fatalf("MarkProjectionWriteStarted after the supersede = %v, want ErrWorkSuperseded", err)
		}
		if got := readWriteMarkerState(ctx, t, control); got != "superseded,false,superseded" {
			t.Fatalf("state = %s, want superseded,false,superseded", got)
		}
	})

	// The lease fence: another attempt's marker marks nothing and reports a
	// lost claim.
	t.Run("lost_claim_marks_nothing", func(t *testing.T) {
		control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
		ctx := context.Background()
		queue := NewProjectorQueue(SQLDB{DB: control}, "proof-worker", time.Minute)
		stale := work
		stale.AttemptCount = 2
		if err := queue.MarkProjectionWriteStarted(ctx, stale); !errors.Is(err, failure.ErrWorkClaimLost) {
			t.Fatalf("MarkProjectionWriteStarted for a stale attempt = %v, want ErrWorkClaimLost", err)
		}
		if got := readWriteMarkerState(ctx, t, control); got != "pending,false,running" {
			t.Fatalf("state = %s, want pending,false,running", got)
		}
	})

	// Latest write start: a later write moves the marker forward, an earlier
	// clock never moves it back.
	t.Run("latest_write_start_never_backwards", func(t *testing.T) {
		control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
		ctx := context.Background()
		first := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
		queue := NewProjectorQueue(SQLDB{DB: control}, "proof-worker", time.Minute)
		for _, at := range []time.Time{first, first.Add(time.Hour), first.Add(-time.Hour)} {
			queue.Now = func() time.Time { return at }
			if err := queue.MarkProjectionWriteStarted(ctx, work); err != nil {
				t.Fatalf("marker at %v = %v", at, err)
			}
		}
		var got time.Time
		if err := control.QueryRowContext(ctx,
			"SELECT projection_write_started_at FROM scope_generations WHERE generation_id = 'gen-il'").Scan(&got); err != nil {
			t.Fatalf("read marker: %v", err)
		}
		if want := first.Add(time.Hour); !got.Equal(want) {
			t.Fatalf("projection_write_started_at = %v, want the latest write start %v", got, want)
		}
	})

	// A lock wait past the marker's lock_timeout changes nothing and reports
	// ErrWorkWriteMarkerDeferred; the re-run succeeds once the row is free.
	t.Run("lock_timeout_is_deferred", func(t *testing.T) {
		control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		holderDB := openSessionOnProofSchema(ctx, t, control, dsn)
		holder, err := holderDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin holder: %v", err)
		}
		defer func() { _ = holder.Rollback() }()
		if _, err := holder.ExecContext(ctx,
			"UPDATE scope_generations SET payload = payload WHERE generation_id = 'gen-il'"); err != nil {
			t.Fatalf("hold the generation row: %v", err)
		}
		queue := NewProjectorQueue(SQLDB{DB: control}, "proof-worker", time.Minute)
		queue.AckScopeLockTimeout = 100 * time.Millisecond
		if err := queue.MarkProjectionWriteStarted(ctx, work); !errors.Is(err, failure.ErrWorkWriteMarkerDeferred) {
			t.Fatalf("MarkProjectionWriteStarted on a held generation row = %v, want ErrWorkWriteMarkerDeferred", err)
		}
		if err := holder.Rollback(); err != nil {
			t.Fatalf("release holder: %v", err)
		}
		if err := queue.MarkProjectionWriteStarted(ctx, work); err != nil {
			t.Fatalf("re-run after the row freed = %v, want nil", err)
		}
		if got := readWriteMarkerState(ctx, t, control); got != "pending,true,running" {
			t.Fatalf("state = %s, want pending,true,running", got)
		}
	})
}

func readWriteMarkerState(ctx context.Context, t *testing.T, database *sql.DB) string {
	t.Helper()
	var state string
	if err := database.QueryRowContext(ctx, readWriteMarkerInterleaveState).Scan(&state); err != nil {
		t.Fatalf("read gen-il state: %v", err)
	}
	return state
}

// TestProjectorHeartbeatWriteGateRowsLive runs the ruling's gate cases on real
// Postgres. Each row seeds gen-il, runs the shipped supersede inside an open
// transaction, probes from a second session whether the generation row is
// locked, and checks the derived gate probe agrees with the outcome.
func TestProjectorHeartbeatWriteGateRowsLive(t *testing.T) {
	dsn := supersessionProofDSN(t)
	const newer = `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-il-newer', 'scope-hb', 'push', now() - interval '20 minutes', now() - interval '20 minutes', 'pending');`
	for _, tc := range []struct {
		name, generation, extra string
		wantSuperseded          bool
		wantClass               string
		wantGenerationLocked    bool
	}{
		{name: "T1_steady_state_no_newer", generation: "'pending', NULL", wantGenerationLocked: false},
		{
			name: "T2_marker_null_newer_pending", generation: "'pending', NULL", extra: newer,
			wantSuperseded: true, wantClass: "projector_superseded_by_newer_generation", wantGenerationLocked: true,
		},
		{name: "T3_marker_set_newer_pending", generation: "'pending', now() - interval '1 minute'", extra: newer},
		{
			name: "T4_own_generation_superseded_marker_set", generation: "'superseded', now() - interval '1 minute'",
			wantSuperseded: true, wantClass: projectorHeartbeatGenerationSupersededClass, wantGenerationLocked: true,
		},
		{name: "E_active_reprojection_marker_set_newer_pending", generation: "'active', now() - interval '1 minute'", extra: newer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control := heartbeatProofDB(t, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, projection_write_started_at)
VALUES ('gen-il', 'scope-hb', 'push', now() - interval '30 minutes', now() - interval '30 minutes', `+tc.generation+`);`+tc.extra+`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at
) VALUES ('projector_scope-hb_gen-il', 'scope-hb', 'gen-il', 'projector', 'source_local',
          'running', 1, 'proof-worker', now() + interval '1 hour', now(), '{}'::jsonb, now(), now());`)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probeDB := openSessionOnProofSchema(ctx, t, control, dsn)

			gateTx, err := control.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin gate probe: %v", err)
			}
			var gateRows int
			if err := gateTx.QueryRowContext(ctx, derivedWriteGateProbeQuery(t), time.Now().UTC(), "scope-hb", "gen-il", "proof-worker", 1).Scan(&gateRows); err != nil {
				t.Fatalf("derived gate probe: %v", err)
			}
			_ = gateTx.Rollback()
			if (gateRows == 1) != tc.wantSuperseded {
				t.Fatalf("derived gate probe selected %d rows, want superseded=%t", gateRows, tc.wantSuperseded)
			}

			tx, err := control.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin supersede: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			rows, err := tx.QueryContext(ctx, supersedeRunningProjectorWorkQuery,
				time.Now().UTC(), "scope-hb", "gen-il", "proof-worker", 1)
			if err != nil {
				t.Fatalf("supersede: %v", err)
			}
			superseded := rows.Next()
			_ = rows.Close()
			if superseded != tc.wantSuperseded {
				t.Fatalf("supersede returned a row = %t, want %t", superseded, tc.wantSuperseded)
			}
			_, lockErr := probeDB.ExecContext(ctx,
				"SELECT 1 FROM scope_generations WHERE generation_id = 'gen-il' FOR NO KEY UPDATE NOWAIT")
			if locked := lockErr != nil; locked != tc.wantGenerationLocked {
				t.Fatalf("generation row locked by the supersede = %t (probe err %v), want %t", locked, lockErr, tc.wantGenerationLocked)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("commit supersede: %v", err)
			}
			var workStatus, class string
			if err := control.QueryRowContext(ctx, `SELECT status, COALESCE(failure_class, '')
FROM fact_work_items WHERE work_item_id = 'projector_scope-hb_gen-il'`).Scan(&workStatus, &class); err != nil {
				t.Fatalf("read work: %v", err)
			}
			if tc.wantSuperseded && (workStatus != "superseded" || class != tc.wantClass) {
				t.Fatalf("work = %s/%s, want superseded/%s", workStatus, class, tc.wantClass)
			}
			if !tc.wantSuperseded && workStatus != "running" {
				t.Fatalf("work status = %s, want running", workStatus)
			}
		})
	}
}
