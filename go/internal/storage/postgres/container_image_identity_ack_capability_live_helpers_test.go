// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func seedContainerImageIdentityAckScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    $1, 'repository', 'git', $1, 'reducer', $1,
    clock_timestamp(), clock_timestamp(), 'active'
)
`, scopeID); err != nil {
		t.Fatalf("seed ACK attempt fence scope: %v", err)
	}
}

func seedContainerImageIdentityAckGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    $2, $1, 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), 'active'
)
`, scopeID, generationID); err != nil {
		t.Fatalf("seed ACK attempt fence generation %s: %v", generationID, err)
	}
}

func seedContainerImageIdentityAckWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	scopeID string,
	generationID string,
	leaseOwner string,
	claimUntil time.Time,
	updatedAt time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    lease_owner, claim_until, payload, created_at, updated_at
) VALUES (
    $1, $2, $3, 'reducer', 'container_image_identity',
    'intent', $1, 'claimed', 1,
    $4, $5,
    jsonb_build_object(
        'entity_key', $1::text,
        'reason', 'synthetic mixed-version ACK proof',
        'fact_id', $1::text,
        'source_system', 'git'
    ),
    $6, $6
)
`, workItemID, scopeID, generationID, leaseOwner, claimUntil, updatedAt); err != nil {
		t.Fatalf("seed ACK attempt fence work item %s: %v", workItemID, err)
	}
	var claimEpochExists bool
	if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_attribute
    WHERE attrelid = 'fact_work_items'::regclass
      AND attname = 'container_image_identity_claim_epoch'
      AND NOT attisdropped
)
`).Scan(&claimEpochExists); err != nil {
		t.Fatalf("inspect ACK claim epoch column %s: %v", workItemID, err)
	}
	if claimEpochExists {
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = 1
WHERE work_item_id = $1
`, workItemID); err != nil {
			t.Fatalf("seed ACK claim epoch %s: %v", workItemID, err)
		}
	}
}

func insertContainerImageIdentityCutoverMarker(
	t *testing.T,
	ctx context.Context,
	db interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	},
	scopeID string,
	generationID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id,
    generation_id,
    activated_by_work_item_id,
    activated_by_claim_epoch
)
SELECT
    $1,
    $2,
    work_item_id,
    container_image_identity_claim_epoch
FROM fact_work_items
WHERE scope_id = $1
  AND generation_id = $2
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeID, generationID); err != nil {
		t.Fatalf("insert ACK attempt fence cutover marker: %v", err)
	}
}

func insertContainerImageIdentityV2Fact(
	t *testing.T,
	ctx context.Context,
	db interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	},
	scopeID string,
	generationID string,
	suffix string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    $3, $1, $2, 'reducer_container_image_identity', $3,
    'reducer', 'git', $3, clock_timestamp(), clock_timestamp(),
    jsonb_build_object(
        'identity_format', 'image_ref_v2',
        'image_ref', 'registry.example.com/team/api:prod'
    )
)
`, scopeID, generationID, "reducer_container_image_identity:5854-v2:"+suffix); err != nil {
		t.Fatalf("insert ACK attempt fence v2 fact: %v", err)
	}
}

func containerImageIdentityAckLegacyFactInsertSQL(suffix string) string {
	return fmt.Sprintf(`
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    'reducer_container_image_identity:5854-legacy:%s',
    $1, $2, 'reducer_container_image_identity',
    'reducer_container_image_identity:5854-legacy:%s',
    'reducer', 'git',
    'reducer_container_image_identity:5854-legacy:%s',
    clock_timestamp(), clock_timestamp(),
    '{"image_ref":"registry.example.com/team/api:prod","outcome":"tag_resolved"}'
)
`, suffix, suffix, suffix)
}

func completeContainerImageIdentityAckCutover(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	suffix string,
) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin ACK-before-marker convergence transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	insertContainerImageIdentityCutoverMarker(t, ctx, tx, scopeID, generationID)
	insertContainerImageIdentityV2Fact(t, ctx, tx, scopeID, generationID, suffix)
	if _, err := tx.ExecContext(ctx, `
DELETE FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = 'reducer_container_image_identity'
  AND COALESCE(payload->>'identity_format', '') <> 'image_ref_v2'
`, scopeID, generationID); err != nil {
		t.Fatalf("retire legacy fact after ACK-before-marker ordering: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit ACK-before-marker convergence transaction: %v", err)
	}
}

func assertContainerImageIdentityAckRowsAffected(
	t *testing.T,
	result sql.Result,
	want int64,
) {
	t.Helper()
	got, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("read ACK attempt fence rows affected: %v", err)
	}
	if got != want {
		t.Fatalf("ACK attempt fence rows affected = %d, want %d", got, want)
	}
}

func assertContainerImageIdentityAckWorkItemState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	wantStatus string,
	wantLeaseOwner string,
) {
	t.Helper()
	var (
		status     string
		leaseOwner sql.NullString
	)
	if err := db.QueryRowContext(ctx, `
SELECT status, lease_owner
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&status, &leaseOwner); err != nil {
		t.Fatalf("read ACK attempt fence work item %s: %v", workItemID, err)
	}
	if status != wantStatus || leaseOwner.String != wantLeaseOwner {
		t.Fatalf(
			"ACK attempt fence work item %s = status %s owner %q, want %s/%q",
			workItemID,
			status,
			leaseOwner.String,
			wantStatus,
			wantLeaseOwner,
		)
	}
}

func assertContainerImageIdentityAckFactCount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	identityFormat string,
	want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = 'reducer_container_image_identity'
  AND COALESCE(payload->>'identity_format', '') = $3
`, scopeID, generationID, identityFormat).Scan(&got); err != nil {
		t.Fatalf("count ACK attempt fence fact rows: %v", err)
	}
	if got != want {
		t.Fatalf(
			"ACK attempt fence fact rows for %s/%s format %q = %d, want %d",
			scopeID,
			generationID,
			identityFormat,
			got,
			want,
		)
	}
}
