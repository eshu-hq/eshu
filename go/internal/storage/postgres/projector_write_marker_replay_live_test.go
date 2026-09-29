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
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// replayProofDB returns a fully migrated private schema holding scope s-rp.
func replayProofDB(t *testing.T) *sql.DB {
	t.Helper()
	database := openClaimDeadlockProofDB(t, supersessionProofDSN(t), 2)
	seedClaimMaintenanceScopes(t, database, "s-rp")
	return database
}

// seedReplayGeneration inserts one s-rp generation ingested ingestedAgo before
// now, with status, is_delta false, an optional activation, and an optional
// write-start marker, plus its projector work row in workStatus.
func seedReplayGeneration(t *testing.T, database *sql.DB, id, status string, ingestedAgo time.Duration,
	activated bool, marker *time.Time, workStatus string,
) {
	t.Helper()
	at := time.Now().UTC().Add(-ingestedAgo)
	var activatedAt any
	if activated {
		activatedAt = at
	}
	var pws any
	if marker != nil {
		pws = *marker
	}
	if _, err := database.Exec(`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at,
    status, activated_at, projection_write_started_at)
VALUES ($1, 's-rp', 'push', false, $2, $2, $3, $4, $5)`, id, at, status, activatedAt, pws); err != nil {
		t.Fatalf("seed generation %s: %v", id, err)
	}
	if activated && status == "active" {
		if _, err := database.Exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = 's-rp'`, id); err != nil {
			t.Fatalf("point scope at %s: %v", id, err)
		}
	}
	if _, err := database.Exec(`
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    failure_class, visible_at, payload, created_at, updated_at, last_attempt_at)
VALUES ($1, 's-rp', $2, 'projector', 'source_local', $3, 1,
    CASE WHEN $3 = 'dead_letter' THEN 'projection_bug' END, $4, '{}'::jsonb, $4, $4, $4)`,
		projectorWorkItemID("s-rp", id), id, workStatus, at); err != nil {
		t.Fatalf("seed work %s: %v", id, err)
	}
}

// claimReplayed claims the replayed s-rp projector row.
func claimReplayed(ctx context.Context, t *testing.T, queue ProjectorQueue, generationID string) projector.ScopeGenerationWork {
	t.Helper()
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != generationID {
		t.Fatalf("Claim() = %+v, %t, %v; want the replayed %s", work.Generation, ok, err, generationID)
	}
	return work
}

func readMarker(ctx context.Context, t *testing.T, database *sql.DB, id string) (string, sql.NullTime, bool) {
	t.Helper()
	var status string
	var pws sql.NullTime
	var activated bool
	if err := database.QueryRowContext(ctx, `SELECT status, projection_write_started_at, activated_at IS NOT NULL
FROM scope_generations WHERE generation_id = $1`, id).Scan(&status, &pws, &activated); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return status, pws, activated
}

// TestReplayedFailedGenerationWritesAndActivatesLive is the #7389 P1
// regression: a dead-lettered projector row leaves its generation failed, and
// replay (the operator path or the poison auto-retry) makes it claimable
// again. The marker must accept the failed generation so the attempt writes
// and Ack activates it; refusing it left the work running and the generation
// never activated.
func TestReplayedFailedGenerationWritesAndActivatesLive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		replay func(context.Context, *sql.DB) error
	}{
		{name: "operator_replay", replay: func(ctx context.Context, database *sql.DB) error {
			result, err := NewRecoveryStore(SQLDB{DB: database}).ReplayFailedWorkItems(ctx,
				recovery.ReplayFilter{Stage: recovery.StageProjector}, time.Now().UTC())
			if err == nil && result.Replayed != 1 {
				return errors.New("operator replay moved no row")
			}
			return err
		}},
		{name: "poison_auto_retry", replay: func(ctx context.Context, database *sql.DB) error {
			_, err := NewPoisonLivenessStore(SQLDB{DB: database}).RecoverPoisonDeadLetters(ctx,
				PoisonLivenessPolicy{MaxRecoverAttempts: 1, BatchLimit: 10}, time.Now().UTC())
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			database := replayProofDB(t)
			seedReplayGeneration(t, database, "gen-f", "failed", time.Hour, false, nil, "dead_letter")
			if err := tc.replay(ctx, database); err != nil {
				t.Fatalf("replay: %v", err)
			}
			queue := NewProjectorQueue(SQLDB{DB: database}, "worker-rp", time.Minute)
			work := claimReplayed(ctx, t, queue, "gen-f")
			if err := queue.MarkProjectionWriteStarted(ctx, work); err != nil {
				t.Fatalf("MarkProjectionWriteStarted(replayed failed generation) = %v, want nil", err)
			}
			if _, pws, _ := readMarker(ctx, t, database, "gen-f"); !pws.Valid {
				t.Fatal("marker not set on the replayed failed generation")
			}
			if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
				t.Fatalf("Ack(replayed failed generation) = %v, want nil", err)
			}
			if status, _, activated := readMarker(ctx, t, database, "gen-f"); status != "active" || !activated {
				t.Fatalf("gen-f = %s activated=%t, want active with activated_at", status, activated)
			}
			writers, err := NewIngestionStore(SQLDB{DB: database}).UncoveredProjectionWriters(ctx, "s-rp")
			if err != nil || len(writers) != 0 {
				t.Fatalf("UncoveredProjectionWriters() = %v, %v; want none", writers, err)
			}
		})
	}
}

// TestReplayAfterFullRecordsLatestWriteStartLive is the replay-after-full
// counterexample to a keep-first marker: G_F wrote at t1 and dead-lettered,
// full G_X wrote at t2 > t1 and activated, then G_F is replayed, writes at
// t3 > t2 over G_X's tree, and fails again. The marker must record t3, so the
// probe reports G_F; keep-first left t1 < t2 and reported the scope clean.
// The marker must also never move backwards.
func TestReplayAfterFullRecordsLatestWriteStartLive(t *testing.T) {
	ctx := context.Background()
	database := replayProofDB(t)
	t1 := time.Now().UTC().Add(-50 * time.Minute).Truncate(time.Microsecond)
	t2 := t1.Add(20 * time.Minute)
	t3 := t2.Add(20 * time.Minute)
	seedReplayGeneration(t, database, "gen-x", "active", 40*time.Minute, true, &t2, "succeeded")
	seedReplayGeneration(t, database, "gen-f", "failed", 30*time.Minute, false, &t1, "dead_letter")
	if _, err := NewRecoveryStore(SQLDB{DB: database}).ReplayFailedWorkItems(ctx,
		recovery.ReplayFilter{Stage: recovery.StageProjector}, time.Now().UTC()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "worker-rp", time.Minute)
	work := claimReplayed(ctx, t, queue, "gen-f")
	queue.Now = func() time.Time { return t3 }
	if err := queue.MarkProjectionWriteStarted(ctx, work); err != nil {
		t.Fatalf("MarkProjectionWriteStarted(replay) = %v", err)
	}
	queue.Now = func() time.Time { return t1.Add(-time.Hour) }
	if err := queue.MarkProjectionWriteStarted(ctx, work); err != nil {
		t.Fatalf("MarkProjectionWriteStarted(earlier clock) = %v", err)
	}
	queue.Now = nil
	if err := queue.Fail(ctx, work, errors.New("projection failed again")); err != nil {
		t.Fatalf("Fail(replay) = %v", err)
	}
	writers, err := NewIngestionStore(SQLDB{DB: database}).UncoveredProjectionWriters(ctx, "s-rp")
	if err != nil || len(writers) != 1 || writers[0].GenerationID != "gen-f" {
		t.Errorf("UncoveredProjectionWriters() = %+v, %v; want [gen-f]: it wrote over gen-x after gen-x's write", writers, err)
	}
	if _, pws, _ := readMarker(ctx, t, database, "gen-f"); !pws.Valid || !pws.Time.Equal(t3) {
		t.Errorf("gen-f projection_write_started_at = %v, want the latest write start %v (never backwards)", pws, t3)
	}
}
