// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestProjectorHeartbeatSupersessionPreservesActivePointer proves that a newer
// pending generation stops stale work without retiring the currently published
// generation before the successor is acknowledged.
func TestProjectorHeartbeatSupersessionPreservesActivePointer(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	for _, tc := range []struct {
		name                  string
		oldStatus             string
		activePointer         any
		successorFails        bool
		oldTerminalFailsFirst bool
	}{
		{name: "published_generation", oldStatus: "active", activePointer: "gen-old"},
		{name: "failed_successor", oldStatus: "active", activePointer: "gen-old", successorFails: true},
		{name: "old_terminal_failure_first", oldStatus: "active", activePointer: "gen-old", oldTerminalFailsFirst: true},
		{name: "unpublished_generation", oldStatus: "pending", activePointer: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, database, "")
			ctx := context.Background()
			oldTime := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
			newTime := oldTime.Add(time.Minute)
			if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-proof', 'repository', 'github', 'proof/repo', 'git',
          'proof/repo', $1, $1, $2, $3)
`, oldTime, tc.oldStatus, tc.activePointer); err != nil {
				t.Fatalf("insert scope: %v", err)
			}
			if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES
    ('gen-old', 'scope-proof', 'push', $1::timestamptz, $1::timestamptz, $2,
        CASE WHEN $2 = 'active' THEN $1::timestamptz ELSE NULL END),
    ('gen-new', 'scope-proof', 'push', $3, $3, 'pending', NULL)
`, oldTime, tc.oldStatus, newTime); err != nil {
				t.Fatalf("insert generations: %v", err)
			}
			if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at
) VALUES
    ('work-old', 'scope-proof', 'gen-old', 'projector', 'source_local',
        'running', 1, 'proof-worker', $2, $1, '{}'::jsonb, $1, $1),
    ('work-new', 'scope-proof', 'gen-new', 'projector', 'source_local',
        'pending', 0, NULL, NULL, $1, '{}'::jsonb, $1, $1)
`, oldTime, newTime.Add(time.Minute)); err != nil {
				t.Fatalf("insert work: %v", err)
			}

			queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
			queue.Now = func() time.Time { return newTime }
			oldWork := projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-proof"},
				Generation:   scope.ScopeGeneration{GenerationID: "gen-old"},
				AttemptCount: 1,
			}
			if tc.oldTerminalFailsFirst {
				if err := queue.Fail(ctx, oldWork, errors.New("terminal active generation failure")); err != nil {
					t.Fatalf("Fail active generation before Heartbeat: %v", err)
				}
				if err := queue.Heartbeat(ctx, oldWork); !errors.Is(err, ErrProjectorClaimRejected) {
					t.Fatalf("Heartbeat after terminal Fail = %v; want claim rejection", err)
				}
				var oldStatus, workStatus, successorStatus string
				var pointer sql.NullString
				if err := database.QueryRowContext(ctx, `
SELECT old_generation.status, old_work.status, new_generation.status,
       scope.active_generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS old_generation ON old_generation.generation_id = 'gen-old'
JOIN scope_generations AS new_generation ON new_generation.generation_id = 'gen-new'
JOIN fact_work_items AS old_work ON old_work.work_item_id = 'work-old'
WHERE scope.scope_id = 'scope-proof'
`).Scan(&oldStatus, &workStatus, &successorStatus, &pointer); err != nil {
					t.Fatalf("read terminal failure state: %v", err)
				}
				if oldStatus != "failed" || workStatus != "dead_letter" ||
					successorStatus != "pending" || pointer.Valid {
					t.Fatalf("old=%q work=%q successor=%q pointer=%v; want terminal failure to invalidate old publication",
						oldStatus, workStatus, successorStatus, pointer)
				}
				return
			}
			if err := queue.Heartbeat(ctx, oldWork); !errors.Is(err, failure.ErrWorkSuperseded) {
				t.Fatalf("old Heartbeat error = %v; want ErrWorkSuperseded", err)
			}

			var oldGenerationStatus, oldWorkStatus, newGenerationStatus string
			var oldSupersededAt sql.NullTime
			var activePointer sql.NullString
			if err := database.QueryRowContext(ctx, `
SELECT old_generation.status, old_generation.superseded_at, old_work.status,
       new_generation.status, scope.active_generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS old_generation ON old_generation.generation_id = 'gen-old'
JOIN scope_generations AS new_generation ON new_generation.generation_id = 'gen-new'
JOIN fact_work_items AS old_work ON old_work.work_item_id = 'work-old'
WHERE scope.scope_id = 'scope-proof'
`).Scan(&oldGenerationStatus, &oldSupersededAt, &oldWorkStatus,
				&newGenerationStatus, &activePointer); err != nil {
				t.Fatalf("read state after Heartbeat: %v", err)
			}
			if oldWorkStatus != "superseded" || newGenerationStatus != "pending" {
				t.Fatalf("work=%q successor=%q; want superseded work and pending successor",
					oldWorkStatus, newGenerationStatus)
			}
			if tc.oldStatus == "active" {
				if oldGenerationStatus != "active" || oldSupersededAt.Valid || !activePointer.Valid || activePointer.String != "gen-old" {
					t.Fatalf("published generation=%q superseded_at=%v pointer=%v; want active until Ack",
						oldGenerationStatus, oldSupersededAt, activePointer)
				}
			} else if oldGenerationStatus != "superseded" || !oldSupersededAt.Valid || activePointer.Valid {
				t.Fatalf("unpublished generation=%q superseded_at=%v pointer=%v; want superseded without pointer",
					oldGenerationStatus, oldSupersededAt, activePointer)
			}

			if tc.oldStatus != "active" {
				return
			}
			if err := queue.Ack(ctx, oldWork, runtime.Result{}); !errors.Is(err, ErrProjectorClaimRejected) {
				t.Fatalf("Ack superseded old work error = %v; want claim rejection", err)
			}
			// A late error from the revoked worker must not fail the generation
			// or clear its pointer after Heartbeat has stopped that work.
			if err := queue.Fail(ctx, oldWork, errors.New("stale worker failure")); !errors.Is(err, ErrProjectorClaimRejected) {
				t.Fatalf("Fail superseded old work error = %v; want claim rejection", err)
			}
			if err := database.QueryRowContext(ctx, `
SELECT generation.status, scope.active_generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation ON generation.generation_id = 'gen-old'
WHERE scope.scope_id = 'scope-proof'
`).Scan(&oldGenerationStatus, &activePointer); err != nil {
				t.Fatalf("read state after stale Fail: %v", err)
			}
			if oldGenerationStatus != "active" || !activePointer.Valid || activePointer.String != "gen-old" {
				t.Fatalf("old=%q pointer=%v after stale Fail; want published generation retained",
					oldGenerationStatus, activePointer)
			}
			if _, err := database.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', attempt_count = attempt_count + 1,
    lease_owner = 'proof-worker', claim_until = $1
WHERE work_item_id = 'work-new'
`, newTime.Add(time.Minute)); err != nil {
				t.Fatalf("claim successor: %v", err)
			}
			newWork := projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-proof"},
				Generation:   scope.ScopeGeneration{GenerationID: "gen-new"},
				AttemptCount: 1,
			}
			if tc.successorFails {
				if err := queue.Fail(ctx, newWork, errors.New("terminal successor proof failure")); err != nil {
					t.Fatalf("Fail successor: %v", err)
				}
				var successorWorkStatus string
				if err := database.QueryRowContext(ctx, `
SELECT generation.status, scope.active_generation_id, work.status
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation ON generation.generation_id = 'gen-old'
JOIN fact_work_items AS work ON work.work_item_id = 'work-new'
WHERE scope.scope_id = 'scope-proof'
`).Scan(&oldGenerationStatus, &activePointer, &successorWorkStatus); err != nil {
					t.Fatalf("read state after successor failure: %v", err)
				}
				if oldGenerationStatus != "active" || !activePointer.Valid ||
					activePointer.String != "gen-old" || successorWorkStatus != "dead_letter" {
					t.Fatalf("old=%q pointer=%v successor work=%q; want current truth retained after Fail",
						oldGenerationStatus, activePointer, successorWorkStatus)
				}
				return
			}
			if err := queue.Ack(ctx, newWork, runtime.Result{}); err != nil {
				t.Fatalf("Ack successor: %v", err)
			}
			if err := database.QueryRowContext(ctx, `
SELECT old_generation.status, new_generation.status, scope.active_generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS old_generation ON old_generation.generation_id = 'gen-old'
JOIN scope_generations AS new_generation ON new_generation.generation_id = 'gen-new'
WHERE scope.scope_id = 'scope-proof'
`).Scan(&oldGenerationStatus, &newGenerationStatus, &activePointer); err != nil {
				t.Fatalf("read state after Ack: %v", err)
			}
			if oldGenerationStatus != "superseded" || newGenerationStatus != "active" ||
				!activePointer.Valid || activePointer.String != "gen-new" {
				t.Fatalf("old=%q successor=%q pointer=%v; want atomic promotion",
					oldGenerationStatus, newGenerationStatus, activePointer)
			}
			if err := queue.Ack(ctx, oldWork, runtime.Result{}); !errors.Is(err, ErrProjectorClaimRejected) {
				t.Fatalf("late Ack superseded old work error = %v; want claim rejection", err)
			}
			if err := database.QueryRowContext(ctx, `
SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = 'scope-proof'
`).Scan(&activePointer); err != nil {
				t.Fatalf("read pointer after late Ack: %v", err)
			}
			if !activePointer.Valid || activePointer.String != "gen-new" {
				t.Fatalf("pointer=%v after late Ack; want successor retained", activePointer)
			}
		})
	}
}
