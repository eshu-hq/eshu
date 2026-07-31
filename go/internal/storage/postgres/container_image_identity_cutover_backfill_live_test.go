// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestContainerImageIdentityCutoverMigrationBackfillsExistingMarkersLive(
	t *testing.T,
) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the cutover backfill proof")
	}

	for _, test := range []struct {
		name          string
		workItemCount int
		wantError     bool
	}{
		{name: "exact stable work item", workItemCount: 1},
		{name: "missing work item", wantError: true},
		{name: "duplicate work items", workItemCount: 2, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			db := openContainerImageIdentityCutoverBackfillProofDB(t, ctx, dsn)
			exec := SQLDB{DB: db}
			preUpgrade, migration := containerImageIdentityCutoverUpgradeDefinitions(t)
			if err := ApplyDefinitions(ctx, exec, preUpgrade); err != nil {
				t.Fatalf("apply pre-088 definitions: %v", err)
			}
			seedContainerImageIdentityCutoverBackfillState(
				t,
				ctx,
				db,
				test.workItemCount,
			)

			err := ApplyDefinitions(ctx, exec, []Definition{migration})
			if test.wantError {
				if err == nil || !strings.Contains(
					err.Error(),
					"requires exactly one reducer work item",
				) {
					t.Fatalf("backfill migration error = %v, want exact-one rejection", err)
				}
				assertContainerImageIdentityBackfillColumnsAbsent(t, ctx, db)
				return
			}
			if err != nil {
				t.Fatalf("apply backfill migration: %v", err)
			}
			var (
				required         bool
				attemptCount     int
				authorizedStatus string
			)
			if err := db.QueryRowContext(ctx, `
SELECT
    container_image_identity_v2_required,
    attempt_count,
    container_image_identity_v2_authorized_status
FROM fact_work_items
WHERE work_item_id = 'ack-5854-cutover-backfill-0'
`).Scan(&required, &attemptCount, &authorizedStatus); err != nil {
				t.Fatalf("read backfilled cutover row: %v", err)
			}
			if !required ||
				attemptCount != 3 ||
				authorizedStatus != "succeeded" {
				t.Fatalf(
					"backfilled row = required %t attempt %d authorized %q, want true/3/succeeded",
					required,
					attemptCount,
					authorizedStatus,
				)
			}
		})
	}
}

func openContainerImageIdentityCutoverBackfillProofDB(
	t *testing.T,
	ctx context.Context,
	dsn string,
) *sql.DB {
	t.Helper()
	schema := fmt.Sprintf("eshu_5854_cutover_backfill_%d", time.Now().UnixNano())
	adminDB := openActiveOCIWarningIndexProofDB(t, dsn)
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create cutover backfill schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE",
		); err != nil {
			t.Errorf("drop cutover backfill schema: %v", err)
		}
	})
	return openActiveOCIWarningIndexProofDB(
		t,
		activeOCIWarningIndexSchemaDSN(t, dsn, schema),
	)
}

func seedContainerImageIdentityCutoverBackfillState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemCount int,
) {
	t.Helper()
	const (
		scopeID      = "repository:5854-cutover-backfill"
		generationID = "generation:5854-cutover-backfill"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
	if _, err := db.ExecContext(ctx, `
CREATE TABLE container_image_identity_cutovers (
    scope_id TEXT NOT NULL
        REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL
        REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    cutover_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (scope_id, generation_id)
)
`); err != nil {
		t.Fatalf("create pre-088 cutover table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (scope_id, generation_id)
VALUES ($1, $2)
`, scopeID, generationID); err != nil {
		t.Fatalf("seed pre-088 cutover marker: %v", err)
	}
	for index := range workItemCount {
		workItemID := fmt.Sprintf("ack-5854-cutover-backfill-%d", index)
		seedContainerImageIdentityAckWorkItem(
			t,
			ctx,
			db,
			workItemID,
			scopeID,
			generationID,
			"reducer",
			time.Date(2026, time.July, 30, 23, 0, 0, 0, time.UTC),
			time.Date(2026, time.July, 30, 22, 0, 0, 0, time.UTC),
		)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded',
    attempt_count = 3,
    lease_owner = NULL,
    claim_until = NULL
WHERE work_item_id = $1
`, workItemID); err != nil {
			t.Fatalf("mark pre-088 work item succeeded: %v", err)
		}
	}
}

func assertContainerImageIdentityBackfillColumnsAbsent(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	var columns int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_attribute
WHERE attrelid = 'fact_work_items'::regclass
  AND attname IN (
      'container_image_identity_v2_required',
      'container_image_identity_v2_authorized_status'
  )
  AND NOT attisdropped
`).Scan(&columns); err != nil {
		t.Fatalf("read rolled-back backfill columns: %v", err)
	}
	if columns != 0 {
		t.Fatalf("rolled-back backfill columns = %d, want 0", columns)
	}
}
