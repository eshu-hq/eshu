// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_ack

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestContainerImageIdentityLegacyAckBatchPerformanceLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	const (
		trials           = 3
		warmups          = 50
		pairedIterations = 400
		batchSize        = 16
		handlerBaseline  = 26023 * time.Microsecond
	)

	for trial := range trials {
		baselineDB := openContainerImageIdentityAckCapabilityProofDB(t)
		candidateDB := openContainerImageIdentityAckCapabilityProofDB(t)
		pinContainerImageIdentityAckTheoryDB(baselineDB)
		pinContainerImageIdentityAckTheoryDB(candidateDB)
		installContainerImageIdentityAckBaselineSchema(t, baselineDB)

		owner := fmt.Sprintf("legacy-reducer-5854-ack-batch-%d", trial)
		ids := make([]string, batchSize)
		for index := range batchSize {
			scopeID := fmt.Sprintf(
				"repository:5854-legacy-ack-batch-%d-%02d",
				trial,
				index,
			)
			generationID := fmt.Sprintf(
				"generation:5854-legacy-ack-batch-%d-%02d",
				trial,
				index,
			)
			ids[index] = fmt.Sprintf(
				"ack-5854-legacy-batch-%d-%02d",
				trial,
				index,
			)
			for _, db := range []*sql.DB{baselineDB, candidateDB} {
				seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
				seedContainerImageIdentityAckGeneration(
					t,
					ctx,
					db,
					scopeID,
					generationID,
				)
				seedContainerImageIdentityAckWorkItem(
					t,
					ctx,
					db,
					ids[index],
					scopeID,
					generationID,
					owner,
					now.Add(time.Minute),
					now,
				)
			}
		}
		prepareContainerImageIdentityAckPerformanceTable(t, baselineDB)
		prepareContainerImageIdentityAckPerformanceTable(t, candidateDB)
		query, args := legacyContainerImageIdentityAckBatchQuery(now, owner, ids)
		before, after := measureContainerImageIdentityAckTwinPair(
			t,
			warmups,
			pairedIterations,
			func() {
				resetContainerImageIdentityAckPerfClaimed(
					t,
					ctx,
					baselineDB,
					owner,
					now,
					ids,
				)
			},
			func() {
				resetContainerImageIdentityAckPerfClaimed(
					t,
					ctx,
					candidateDB,
					owner,
					now,
					ids,
				)
			},
			func() error {
				_, err := baselineDB.ExecContext(ctx, query, args...)
				return err
			},
			func() error {
				_, err := candidateDB.ExecContext(ctx, query, args...)
				return err
			},
		)
		assertContainerImageIdentityAckPerfBudget(
			t,
			"legacy pre-cutover batch16 ACK",
			before,
			after,
		)
		medianContribution := float64(after.median-before.median) /
			float64(batchSize*handlerBaseline)
		t.Logf(
			"ACKLEGACY5854 trial=%d warmups=%d pairs=%d batch=%d before_median_us=%.3f before_p95_us=%.3f after_median_us=%.3f after_p95_us=%.3f handler_contribution_pct=%.4f",
			trial+1,
			warmups,
			pairedIterations,
			batchSize,
			ackPerfMicros(before.median),
			ackPerfMicros(before.p95),
			ackPerfMicros(after.median),
			ackPerfMicros(after.p95),
			medianContribution*100,
		)
	}
}

func installContainerImageIdentityAckBaselineSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
DROP TRIGGER fact_work_items_container_image_identity_claim_epoch_advance
    ON fact_work_items;
ALTER TABLE container_image_identity_cutovers
DISABLE TRIGGER container_image_identity_cutover_marker_guard;
ALTER TABLE fact_work_items
DROP CONSTRAINT fact_work_items_container_image_identity_v2_status_check,
DROP COLUMN container_image_identity_v2_authorized_status,
DROP COLUMN container_image_identity_claim_epoch,
DROP COLUMN container_image_identity_v2_required;
`); err != nil {
		t.Fatalf("install pre-088 ACK baseline schema: %v", err)
	}
}

func prepareContainerImageIdentityAckPerformanceTable(
	t *testing.T,
	db *sql.DB,
) {
	t.Helper()
	if _, err := db.Exec(`
ALTER TABLE fact_work_items SET (
    autovacuum_enabled = FALSE,
    toast.autovacuum_enabled = FALSE
)
`); err != nil {
		t.Fatalf("disable benchmark-table autovacuum: %v", err)
	}
	if _, err := db.Exec("VACUUM (ANALYZE) fact_work_items"); err != nil {
		t.Fatalf("vacuum benchmark table: %v", err)
	}
}

func resetContainerImageIdentityAckAttemptTheoryClaimed(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	owner string,
	now time.Time,
	ids []string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = CASE
        WHEN domain = 'container_image_identity'
            AND container_image_identity_v2_required
            THEN 'running'
        ELSE 'claimed'
    END,
    attempt_count = 1,
    lease_owner = $1,
    claim_until = $2,
    visible_at = NULL,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    container_image_identity_v2_authorized_status = CASE
        WHEN container_image_identity_v2_required THEN 'running'
        ELSE ''
    END,
    updated_at = $3
WHERE work_item_id = ANY($4::text[])
`, owner, now.Add(time.Minute), now, ids); err != nil {
		t.Fatalf("reset attempt-theory ACK claimed rows: %v", err)
	}
}
