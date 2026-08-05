// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"
)

const provenanceUpgradePerformanceBatchSize = 500

// TestProvenanceEdgeIdentityUpgradeTriggerNoRegressionLive compares the same
// affected-domain enqueue, claim, and ACK batch with and without migration
// 096's permanent capability triggers on a real Postgres schema.
func TestProvenanceEdgeIdentityUpgradeTriggerNoRegressionLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID    = "repository:5827-provenance-trigger-performance"
		generation = "generation:5827-provenance-trigger-performance"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate trigger-performance scope: %v", err)
	}

	dropProvenanceUpgradeTriggers(t, ctx, db)
	measureProvenanceUpgradeQueueBatch(
		t, ctx, db, scopeID, generation, "baseline-warmup",
	)
	restoreProvenanceUpgradeTriggers(t, ctx, db)
	measureProvenanceUpgradeQueueBatch(
		t, ctx, db, scopeID, generation, "triggered-warmup",
	)
	baseline, triggered := measureProvenanceUpgradeAlternatingSamples(
		t, ctx, db, scopeID, generation,
	)

	baselineMedian := medianProvenanceUpgradeDuration(baseline)
	triggeredMedian := medianProvenanceUpgradeDuration(triggered)
	overhead := triggeredMedian - baselineMedian
	if overhead < 0 {
		overhead = 0
	}
	t.Logf(
		"provenance capability trigger batch=%d baseline_median=%s triggered_median=%s overhead=%s overhead_per_item=%s",
		provenanceUpgradePerformanceBatchSize,
		baselineMedian,
		triggeredMedian,
		overhead,
		overhead/provenanceUpgradePerformanceBatchSize,
	)

	// The allowance caps permanent database work at the larger of 50 percent
	// or 20 ms per 500-item batch. This absorbs local host jitter while still
	// failing a material queue-path regression.
	allowance := baselineMedian / 2
	if allowance < 20*time.Millisecond {
		allowance = 20 * time.Millisecond
	}
	if triggeredMedian > baselineMedian+allowance {
		t.Fatalf(
			"provenance capability trigger median = %s, baseline %s + allowance %s",
			triggeredMedian,
			baselineMedian,
			allowance,
		)
	}
}

func restoreProvenanceUpgradeTriggers(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(
		ctx,
		MigrationSQL("provenance_edge_identity_upgrade_seed"),
	); err != nil {
		t.Fatalf("restore provenance upgrade triggers: %v", err)
	}
}

func dropProvenanceUpgradeTriggers(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
DROP TRIGGER fact_work_items_require_provenance_edge_identity_insert ON fact_work_items;
DROP TRIGGER fact_work_items_require_provenance_edge_identity_update ON fact_work_items;
DROP TRIGGER fact_work_items_enforce_provenance_edge_identity_upgrade ON fact_work_items
`); err != nil {
		t.Fatalf("drop provenance upgrade triggers for baseline: %v", err)
	}
}

func measureProvenanceUpgradeAlternatingSamples(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generation string,
) ([]time.Duration, []time.Duration) {
	t.Helper()
	const sampleCount = 7
	baseline := make([]time.Duration, 0, sampleCount)
	triggered := make([]time.Duration, 0, sampleCount)
	for sample := range sampleCount {
		if sample%2 == 0 {
			dropProvenanceUpgradeTriggers(t, ctx, db)
			baseline = append(baseline, measureProvenanceUpgradeQueueBatch(
				t, ctx, db, scopeID, generation, fmt.Sprintf("baseline-%d", sample),
			))
			restoreProvenanceUpgradeTriggers(t, ctx, db)
			triggered = append(triggered, measureProvenanceUpgradeQueueBatch(
				t, ctx, db, scopeID, generation, fmt.Sprintf("triggered-%d", sample),
			))
			continue
		}
		triggered = append(triggered, measureProvenanceUpgradeQueueBatch(
			t, ctx, db, scopeID, generation, fmt.Sprintf("triggered-%d", sample),
		))
		dropProvenanceUpgradeTriggers(t, ctx, db)
		baseline = append(baseline, measureProvenanceUpgradeQueueBatch(
			t, ctx, db, scopeID, generation, fmt.Sprintf("baseline-%d", sample),
		))
		restoreProvenanceUpgradeTriggers(t, ctx, db)
	}
	return baseline, triggered
}

func measureProvenanceUpgradeQueueBatch(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generation string,
	prefix string,
) time.Duration {
	t.Helper()
	startedAt := time.Now()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at
)
SELECT
    $1 || '-' || series.i::text,
    $2,
    $3,
    'reducer',
    'package_source_correlation',
    'pending',
    'intent',
    $1 || '-' || series.i::text,
    '{}'::jsonb,
    clock_timestamp(),
    clock_timestamp()
FROM generate_series(1, $4) AS series(i)
`, prefix, scopeID, generation, provenanceUpgradePerformanceBatchSize); err != nil {
		t.Fatalf("insert provenance trigger performance batch %s: %v", prefix, err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed',
    lease_owner = 'provenance-trigger-performance',
    claim_until = clock_timestamp() + interval '1 minute',
    updated_at = clock_timestamp()
WHERE work_item_id LIKE $1 || '-%'
  AND status = 'pending'
`, prefix); err != nil {
		t.Fatalf("claim provenance trigger performance batch %s: %v", prefix, err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded',
    provenance_edge_identity_upgrade_required = FALSE,
    lease_owner = NULL,
    claim_until = NULL,
    updated_at = clock_timestamp()
WHERE work_item_id LIKE $1 || '-%'
  AND status = 'claimed'
`, prefix); err != nil {
		t.Fatalf("ACK provenance trigger performance batch %s: %v", prefix, err)
	}
	elapsed := time.Since(startedAt)

	if _, err := db.ExecContext(
		ctx,
		"DELETE FROM fact_work_items WHERE work_item_id LIKE $1 || '-%'",
		prefix,
	); err != nil {
		t.Fatalf("clean provenance trigger performance batch %s: %v", prefix, err)
	}
	return elapsed
}

func medianProvenanceUpgradeDuration(samples []time.Duration) time.Duration {
	ordered := append([]time.Duration(nil), samples...)
	slices.Sort(ordered)
	return ordered[len(ordered)/2]
}
