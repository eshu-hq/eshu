// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestContainerImageIdentityCutoverMigrationAcceptsDurableClaimLatchWithoutMarkerLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-unmarked-claim-latch"
		generationID = "generation:5854-unmarked-claim-latch"
		workItemID   = "work-5854-unmarked-claim-latch"
		owner        = "reducer-5854-unmarked-claim-latch"
	)
	now := time.Date(2026, time.July, 31, 13, 30, 0, 0, time.UTC)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, workItemID, scopeID, generationID,
		owner, now.Add(time.Minute), now,
	)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_v2_required = TRUE,
    container_image_identity_v2_authorized_status = 'claimed'
WHERE work_item_id = $1
`, workItemID); err != nil {
		t.Fatalf("seed durable pre-marker claim latch: %v", err)
	}

	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("reapply migration with valid pre-marker claim latch: %v", err)
	}
	assertContainerImageIdentityClaimLatchState(
		t, ctx, db, workItemID, "claimed", 1, true, "claimed",
	)
	assertContainerImageIdentityAckOrderingMarkerCount(
		t, ctx, db, scopeID, generationID, 0,
	)
}

func proveContainerImageIdentityCutoverMigrationRerunStates(
	t *testing.T,
	ctx context.Context,
	exec Executor,
	db *sql.DB,
) {
	t.Helper()

	for _, queueStatus := range []string{
		"pending",
		"running",
		"retrying",
		"succeeded",
		"failed",
		"dead_letter",
		"superseded",
	} {
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = $1,
    container_image_identity_v2_authorized_status = $1
WHERE scope_id = 'repository:5854-cutover-migration'
  AND generation_id = 'generation:5854-cutover-migration'
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
`, queueStatus); err != nil {
			t.Fatalf("set cutover work item to %s: %v", queueStatus, err)
		}

		if err := ApplyBootstrap(ctx, exec); err != nil {
			t.Fatalf(
				"reapply migration 088 with authorized %s cutover work item: %v",
				queueStatus,
				err,
			)
		}

		var (
			gotStatus     string
			gotAuthorized string
			gotRequired   bool
		)
		if err := db.QueryRowContext(ctx, `
SELECT
    status,
    container_image_identity_v2_authorized_status,
    container_image_identity_v2_required
FROM fact_work_items
WHERE scope_id = 'repository:5854-cutover-migration'
  AND generation_id = 'generation:5854-cutover-migration'
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
`).Scan(&gotStatus, &gotAuthorized, &gotRequired); err != nil {
			t.Fatalf("read %s cutover work item after migration rerun: %v", queueStatus, err)
		}
		if gotStatus != queueStatus || gotAuthorized != queueStatus || !gotRequired {
			t.Fatalf(
				"cutover work item after %s rerun = status %q authorized %q required %t",
				queueStatus,
				gotStatus,
				gotAuthorized,
				gotRequired,
			)
		}
	}

	if _, err := db.ExecContext(ctx, `
ALTER TABLE fact_work_items
DROP CONSTRAINT fact_work_items_container_image_identity_v2_status_check
`); err != nil {
		t.Fatalf("remove queue-fence constraint for partial-upgrade rerun proof: %v", err)
	}

	for _, test := range []struct {
		name       string
		status     string
		authorized string
	}{
		{
			name:       "unknown equal lifecycle",
			status:     "paused",
			authorized: "paused",
		},
		{
			name:       "known lifecycle mismatch",
			status:     "running",
			authorized: "succeeded",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = $1,
    container_image_identity_v2_authorized_status = $2
WHERE scope_id = 'repository:5854-cutover-migration'
  AND generation_id = 'generation:5854-cutover-migration'
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
`, test.status, test.authorized); err != nil {
				t.Fatalf("seed invalid rerun state: %v", err)
			}

			err := ApplyBootstrap(ctx, exec)
			var sqlState interface{ SQLState() string }
			if err == nil ||
				!errors.As(err, &sqlState) ||
				sqlState.SQLState() != "55000" ||
				!strings.Contains(
					err.Error(),
					"cutover has invalid queue fence state",
				) {
				t.Fatalf(
					"reapply migration 088 with %s/%s = %v, want SQLSTATE 55000 queue-fence rejection",
					test.status,
					test.authorized,
					err,
				)
			}

			var gotStatus, gotAuthorized string
			if err := db.QueryRowContext(ctx, `
SELECT status, container_image_identity_v2_authorized_status
FROM fact_work_items
WHERE scope_id = 'repository:5854-cutover-migration'
  AND generation_id = 'generation:5854-cutover-migration'
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
`).Scan(&gotStatus, &gotAuthorized); err != nil {
				t.Fatalf("read rejected rerun state: %v", err)
			}
			if gotStatus != test.status || gotAuthorized != test.authorized {
				t.Fatalf(
					"rejected rerun mutated state to %s/%s, want %s/%s",
					gotStatus,
					gotAuthorized,
					test.status,
					test.authorized,
				)
			}
		})
	}
}
