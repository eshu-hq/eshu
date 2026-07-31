// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func proveContainerImageIdentityCutoverWorkItemLockRollback(
	t *testing.T,
	ctx context.Context,
	exec SQLDB,
	migration Definition,
	db *sql.DB,
) {
	t.Helper()

	blockingTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin fact_work_items migration blocker: %v", err)
	}
	if _, err := blockingTx.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    lease_owner, claim_until, created_at, updated_at
) VALUES (
    'cutover-migration-blocking-work-item',
    'repository:5854-cutover-migration',
    'generation:5854-cutover-migration',
    'reducer',
    'container_image_identity',
    'claimed',
    'legacy-reducer-5854',
    clock_timestamp() + interval '1 minute',
    clock_timestamp(),
    clock_timestamp()
)
`); err != nil {
		_ = blockingTx.Rollback()
		t.Fatalf("hold populated fact_work_items writer lock: %v", err)
	}

	lockErr := ApplyDefinitionsWithLockTimeout(
		ctx,
		exec,
		[]Definition{migration},
		100*time.Millisecond,
	)
	if lockErr == nil || !strings.Contains(strings.ToLower(lockErr.Error()), "lock timeout") {
		_ = blockingTx.Rollback()
		t.Fatalf("migration 088 under active work-item writer = %v, want bounded lock timeout", lockErr)
	}
	assertContainerImageIdentityCutoverObjectsAbsent(t, ctx, db)
	if err := blockingTx.Rollback(); err != nil {
		t.Fatalf("release fact_work_items migration blocker: %v", err)
	}
}
