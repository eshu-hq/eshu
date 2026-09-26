// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// replaySupersededSeedSQL seeds one scope whose old generation a newer Ack
// superseded. Each generation has a dead-lettered projector row, and the old
// one also has a dead-lettered reducer row.
const replaySupersededSeedSQL = `
ALTER TABLE fact_work_items
    ADD COLUMN container_image_identity_v2_required boolean NOT NULL DEFAULT false,
    ADD COLUMN container_image_identity_v2_authorized_status text NOT NULL DEFAULT '',
    ADD COLUMN container_image_identity_v3_required boolean NOT NULL DEFAULT false,
    ADD COLUMN container_image_identity_v3_authorized_status text NOT NULL DEFAULT '';
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-replay', 'repository', 'github', 'proof/replay', 'git',
          'proof/replay', now(), now(), 'active', 'gen-new');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, superseded_at
) VALUES ('gen-old', 'scope-replay', 'push', now() - interval '1 hour',
          now() - interval '1 hour', 'superseded', now() - interval '1 hour', now()),
         ('gen-new', 'scope-replay', 'push', now(), now(), 'active', now(), NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, failure_class, payload, created_at, updated_at
) VALUES
    ('projector-old', 'scope-replay', 'gen-old', 'projector', 'source_local',
     'dead_letter', 3, 'retry_exhausted', '{}'::jsonb, now(), now()),
    ('projector-new', 'scope-replay', 'gen-new', 'projector', 'source_local',
     'dead_letter', 3, 'retry_exhausted', '{}'::jsonb, now(), now()),
    ('reducer-old', 'scope-replay', 'gen-old', 'reducer', 'workload_identity',
     'dead_letter', 3, 'retry_exhausted', '{}'::jsonb, now(), now());
`

// TestReplayLeavesSupersededGenerationProjectorWork is the #7130 replay fence.
// A replay must not move a projector row back to pending when its generation
// is superseded: claimed and acked, that row would try to re-activate the
// retired generation. The drain's backlog count must agree with the replay,
// and the skip must be visible to the operator.
func TestReplayLeavesSupersededGenerationProjectorWork(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	for _, tc := range []struct {
		name  string
		limit int
	}{
		{name: "unbounded", limit: 0},
		{name: "bounded_drain", limit: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, database, replaySupersededSeedSQL)
			instruments, reader := newEnqueueInstruments(t)
			store := NewRecoveryStore(SQLDB{DB: database}, WithRecoveryInstruments(instruments))
			ctx := context.Background()
			filter := recovery.ReplayFilter{Stage: recovery.StageProjector, Limit: tc.limit}

			depth, err := store.CountDeadLetterBacklog(ctx, filter)
			if err != nil {
				t.Fatalf("CountDeadLetterBacklog: %v", err)
			}
			if depth != 1 {
				t.Fatalf("projector backlog depth = %d, want 1 (superseded-generation row excluded)", depth)
			}
			result, err := store.ReplayFailedWorkItems(ctx, filter, time.Now())
			if err != nil {
				t.Fatalf("ReplayFailedWorkItems: %v", err)
			}
			if !slices.Equal(result.WorkItemIDs, []string{"projector-new"}) ||
				result.SkippedSupersededGeneration != 1 {
				t.Fatalf("replay = ids %v skipped %d, want [projector-new] and 1 skipped",
					result.WorkItemIDs, result.SkippedSupersededGeneration)
			}
			assertReplayStatus(t, database, "projector-old", "dead_letter")
			assertReplayStatus(t, database, "projector-new", "pending")

			var rm metricdata.ResourceMetrics
			if err := reader.Collect(ctx, &rm); err != nil {
				t.Fatalf("collect metrics: %v", err)
			}
			assertCounterPresentWithLabels(t, rm, "eshu_dp_superseded_generation_fence_total",
				map[string]string{"failure_class": projectorReplayGenerationSupersededClass})

			// The fence is projector-only: reducer work keeps its own
			// generation handling and replays as before.
			reducerResult, err := store.ReplayFailedWorkItems(ctx,
				recovery.ReplayFilter{Stage: recovery.StageReducer, Limit: tc.limit}, time.Now())
			if err != nil {
				t.Fatalf("reducer ReplayFailedWorkItems: %v", err)
			}
			if !slices.Equal(reducerResult.WorkItemIDs, []string{"reducer-old"}) ||
				reducerResult.SkippedSupersededGeneration != 0 {
				t.Fatalf("reducer replay = ids %v skipped %d, want [reducer-old] and 0 skipped",
					reducerResult.WorkItemIDs, reducerResult.SkippedSupersededGeneration)
			}
		})
	}
}

// TestReplayWithOnlyFencedRowsStillReportsTheSkipCount proves the replay
// statement returns the skip count even when it replays nothing: the NULL
// work_item_id row the LEFT JOIN emits must scan against a real Postgres.
func TestReplayWithOnlyFencedRowsStillReportsTheSkipCount(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	for _, limit := range []int{0, 10} {
		database := openLivenessProofDB(t, dsn)
		provisionLivenessSchema(t, database, replaySupersededSeedSQL)
		if _, err := database.ExecContext(context.Background(),
			"DELETE FROM fact_work_items WHERE work_item_id = 'projector-new'"); err != nil {
			t.Fatalf("limit %d: drop the replayable row: %v", limit, err)
		}
		result, err := NewRecoveryStore(SQLDB{DB: database}).ReplayFailedWorkItems(context.Background(),
			recovery.ReplayFilter{Stage: recovery.StageProjector, Limit: limit}, time.Now())
		if err != nil {
			t.Fatalf("limit %d: ReplayFailedWorkItems: %v", limit, err)
		}
		if result.Replayed != 0 || len(result.WorkItemIDs) != 0 || result.SkippedSupersededGeneration != 1 {
			t.Fatalf("limit %d: result = %+v, want nothing replayed and 1 skipped", limit, result)
		}
		assertReplayStatus(t, database, "projector-old", "dead_letter")
	}
}

func assertReplayStatus(t *testing.T, db *sql.DB, workItemID, want string) {
	t.Helper()
	var status string
	if err := db.QueryRowContext(context.Background(),
		"SELECT status FROM fact_work_items WHERE work_item_id = $1", workItemID,
	).Scan(&status); err != nil {
		t.Fatalf("read %s status: %v", workItemID, err)
	}
	if status != want {
		t.Fatalf("%s status = %s, want %s", workItemID, status, want)
	}
}
