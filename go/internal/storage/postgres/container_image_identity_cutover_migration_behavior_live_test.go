// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func proveContainerImageIdentityCutoverMigrationBehavior(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	const (
		legacyFactID = "reducer_container_image_identity:5854-legacy-migration"
		newFactID    = "reducer_container_image_identity:5854-v2-migration"
		scopeID      = "repository:5854-cutover-migration"
		generationID = "generation:5854-cutover-migration"
	)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    $1, $2, $3, 'reducer_container_image_identity', 'legacy:tag',
    'reducer', 'git', 'legacy:tag',
    '2026-07-29T22:01:00Z', '2026-07-29T22:01:00Z',
    '{"image_ref":"registry.example.com/team/api:prod","outcome":"tag_resolved"}'
)`, legacyFactID, scopeID, generationID); err != nil {
		t.Fatalf("insert pre-cutover legacy row: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin identity-cutover proof transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
WITH locked AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(
        hashtextextended($1 || E'\x1f' || $2, 5854)
    )
)
INSERT INTO container_image_identity_cutovers (
    scope_id,
    generation_id,
    activated_by_work_item_id,
    activated_by_claim_epoch
)
SELECT
    $1,
    $2,
    work_item.work_item_id,
    work_item.container_image_identity_claim_epoch
FROM locked
JOIN fact_work_items AS work_item
  ON work_item.scope_id = $1
 AND work_item.generation_id = $2
 AND work_item.stage = 'reducer'
 AND work_item.domain = 'container_image_identity'
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeID, generationID); err != nil {
		t.Fatalf("insert identity-cutover marker: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    $1, $2, $3, 'reducer_container_image_identity', 'image-ref:prod',
    'reducer', 'git', 'image-ref:prod',
    '2026-07-29T22:02:00Z', '2026-07-29T22:02:00Z',
    '{"identity_format":"image_ref_v2","image_ref":"registry.example.com/team/api:prod"}'
)
`, newFactID, scopeID, generationID); err != nil {
		t.Fatalf("insert new-format identity row: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM fact_records WHERE fact_id = $1", legacyFactID); err != nil {
		t.Fatalf("delete pre-cutover legacy row: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit identity-cutover proof transaction: %v", err)
	}

	_, err = db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    $1, $2, $3, 'reducer_container_image_identity', 'legacy:tag',
    'reducer', 'git', 'legacy:tag',
    '2026-07-29T22:03:00Z', '2026-07-29T22:03:00Z',
    '{"image_ref":"registry.example.com/team/api:prod","outcome":"tag_resolved"}'
)
`, legacyFactID, scopeID, generationID)
	var sqlState interface{ SQLState() string }
	if !errors.As(err, &sqlState) || sqlState.SQLState() != "55000" {
		t.Fatalf("post-cutover legacy insert error = %v, want SQLSTATE 55000", err)
	}

	var (
		legacyRows int
		newRows    int
		markers    int
		v2Required bool
	)
	if err := db.QueryRowContext(ctx, `
SELECT
    count(*) FILTER (WHERE fact_id = $1),
    count(*) FILTER (WHERE fact_id = $2),
    (SELECT count(*) FROM container_image_identity_cutovers
     WHERE scope_id = $3 AND generation_id = $4),
    (SELECT container_image_identity_v2_required
     FROM fact_work_items
     WHERE scope_id = $3
       AND generation_id = $4
       AND stage = 'reducer'
       AND domain = 'container_image_identity')
FROM fact_records
`, legacyFactID, newFactID, scopeID, generationID).Scan(
		&legacyRows,
		&newRows,
		&markers,
		&v2Required,
	); err != nil {
		t.Fatalf("read identity-cutover proof result: %v", err)
	}
	if legacyRows != 0 || newRows != 1 || markers != 1 || !v2Required {
		t.Fatalf(
			"identity-cutover rows = legacy %d new %d markers %d v2_required %t, want 0/1/1/true",
			legacyRows,
			newRows,
			markers,
			v2Required,
		)
	}
}
