// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

func TestPostgresContainerImageIdentityMarkerInvalidatesOpenTransactionCache(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	const (
		scopeID      = "repository:5854-cache-marker"
		generationID = "generation:5854-cache-marker"
	)
	seedContainerImageIdentityCutoverCacheParents(t, ctx, db, scopeID, generationID)
	t.Cleanup(func() {
		cleanupContainerImageIdentityCutoverCacheScope(t, db, scopeID)
	})

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve cutover-cache connection: %v", err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin open-marker-open transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	first := containerImageIdentityCutoverCacheLegacyRow(
		"reducer_container_image_identity:5854-cache-marker:first",
		scopeID,
		generationID,
	)
	if err := factwrite.BatchInsertFacts(ctx, tx, []factwrite.Row{first}); err != nil {
		t.Fatalf("prime open cutover cache: %v", err)
	}
	if err := execContainerImageIdentityCutoverFence(
		ctx,
		tx,
		scopeID,
		generationID,
		containerImageIdentityLiveWorkItemID(generationID),
		1,
	); err != nil {
		t.Fatalf("insert marker after open cache: %v", err)
	}
	second := containerImageIdentityCutoverCacheLegacyRow(
		"reducer_container_image_identity:5854-cache-marker:second",
		scopeID,
		generationID,
	)
	assertContainerImageIdentityLegacyStatementRejected(
		t,
		factwrite.BatchInsertFacts(ctx, tx, []factwrite.Row{second}),
	)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("roll back open-marker-open transaction: %v", err)
	}

	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE scope_id = $1`,
		0,
		scopeID,
	)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM container_image_identity_cutovers WHERE scope_id = $1`,
		0,
		scopeID,
	)
}

func TestPostgresContainerImageIdentityCutoverCacheFollowsTransactionBoundaries(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	const (
		scopeID      = "repository:5854-cache-boundaries"
		generationID = "generation:5854-cache-boundaries"
	)
	seedContainerImageIdentityCutoverCacheParents(t, ctx, db, scopeID, generationID)
	t.Cleanup(func() {
		cleanupContainerImageIdentityCutoverCacheScope(t, db, scopeID)
	})

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve cache-boundary connection: %v", err)
	}
	defer conn.Close()

	rolledBack, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin rolled-back marker transaction: %v", err)
	}
	if err := execContainerImageIdentityCutoverFence(
		ctx,
		rolledBack,
		scopeID,
		generationID,
		containerImageIdentityLiveWorkItemID(generationID),
		1,
	); err != nil {
		_ = rolledBack.Rollback()
		t.Fatalf("insert marker before rollback: %v", err)
	}
	if err := rolledBack.Rollback(); err != nil {
		t.Fatalf("roll back marker transaction: %v", err)
	}
	afterRollback := containerImageIdentityCutoverCacheLegacyRow(
		"reducer_container_image_identity:5854-cache-boundaries:rollback",
		scopeID,
		generationID,
	)
	if err := factwrite.BatchInsertFacts(
		ctx,
		conn,
		[]factwrite.Row{afterRollback},
	); err != nil {
		t.Fatalf("legacy write after marker rollback: %v", err)
	}

	savepointTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin savepoint cache transaction: %v", err)
	}
	defer func() { _ = savepointTx.Rollback() }()
	if _, err := savepointTx.ExecContext(ctx, "SAVEPOINT before_cutover_marker"); err != nil {
		t.Fatalf("create marker savepoint: %v", err)
	}
	if err := execContainerImageIdentityCutoverFence(
		ctx,
		savepointTx,
		scopeID,
		generationID,
		containerImageIdentityLiveWorkItemID(generationID),
		1,
	); err != nil {
		t.Fatalf("insert marker inside savepoint: %v", err)
	}
	if _, err := savepointTx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT before_cutover_marker"); err != nil {
		t.Fatalf("roll back marker savepoint: %v", err)
	}
	afterSavepoint := containerImageIdentityCutoverCacheLegacyRow(
		"reducer_container_image_identity:5854-cache-boundaries:savepoint",
		scopeID,
		generationID,
	)
	if err := factwrite.BatchInsertFacts(
		ctx,
		savepointTx,
		[]factwrite.Row{afterSavepoint},
	); err != nil {
		t.Fatalf("legacy write after marker savepoint rollback: %v", err)
	}
	if err := savepointTx.Commit(); err != nil {
		t.Fatalf("commit savepoint cache transaction: %v", err)
	}

	committed, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin committed marker transaction: %v", err)
	}
	if err := execContainerImageIdentityCutoverFence(
		ctx,
		committed,
		scopeID,
		generationID,
		containerImageIdentityLiveWorkItemID(generationID),
		1,
	); err != nil {
		_ = committed.Rollback()
		t.Fatalf("insert committed marker: %v", err)
	}
	if err := committed.Commit(); err != nil {
		t.Fatalf("commit marker transaction: %v", err)
	}

	conflictTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin marker conflict transaction: %v", err)
	}
	defer func() { _ = conflictTx.Rollback() }()
	if err := execContainerImageIdentityCutoverFence(
		ctx,
		conflictTx,
		scopeID,
		generationID,
		containerImageIdentityLiveWorkItemID(generationID),
		1,
	); err != nil {
		t.Fatalf("repeat marker with ON CONFLICT: %v", err)
	}
	afterCommit := containerImageIdentityCutoverCacheLegacyRow(
		"reducer_container_image_identity:5854-cache-boundaries:commit",
		scopeID,
		generationID,
	)
	assertContainerImageIdentityLegacyStatementRejected(
		t,
		factwrite.BatchInsertFacts(ctx, conflictTx, []factwrite.Row{afterCommit}),
	)
	if err := conflictTx.Rollback(); err != nil {
		t.Fatalf("roll back rejected legacy-write transaction: %v", err)
	}
}

func assertContainerImageIdentityLegacyStatementRejected(t *testing.T, err error) {
	t.Helper()
	var sqlState interface{ SQLState() string }
	if !errors.As(err, &sqlState) || sqlState.SQLState() != "55000" {
		t.Fatalf("legacy statement error = %v, want SQLSTATE 55000", err)
	}
}

func TestPostgresContainerImageIdentityCutoverCacheRechecksAlternatingKeys(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	const (
		scopeA      = "repository:5854-cache-a"
		generationA = "generation:5854-cache-a"
		scopeB      = "repository:5854-cache-b"
		generationB = "generation:5854-cache-b"
	)
	seedContainerImageIdentityCutoverCacheParents(t, ctx, db, scopeA, generationA)
	seedContainerImageIdentityCutoverCacheParents(t, ctx, db, scopeB, generationB)
	t.Cleanup(func() {
		cleanupContainerImageIdentityCutoverCacheScope(t, db, scopeA)
		cleanupContainerImageIdentityCutoverCacheScope(t, db, scopeB)
	})

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin alternating-key transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows := []factwrite.Row{
		containerImageIdentityCutoverCacheLegacyRow(
			"reducer_container_image_identity:5854-cache-a:first",
			scopeA,
			generationA,
		),
		containerImageIdentityCutoverCacheLegacyRow(
			"reducer_container_image_identity:5854-cache-b:first",
			scopeB,
			generationB,
		),
		containerImageIdentityCutoverCacheLegacyRow(
			"reducer_container_image_identity:5854-cache-a:second",
			scopeA,
			generationA,
		),
	}
	for index := range rows {
		if err := factwrite.BatchInsertFacts(ctx, tx, rows[index:index+1]); err != nil {
			t.Fatalf("alternating-key legacy write %d: %v", index, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit alternating-key transaction: %v", err)
	}
}

func seedContainerImageIdentityCutoverCacheParents(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
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
ON CONFLICT (scope_id) DO NOTHING
`, scopeID); err != nil {
		t.Fatalf("seed cache-test scope %s: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    $2, $1, 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), 'active'
)
ON CONFLICT (generation_id) DO NOTHING
`, scopeID, generationID); err != nil {
		t.Fatalf("seed cache-test generation %s: %v", generationID, err)
	}
	seedContainerImageIdentityLiveWorkItem(t, ctx, db, scopeID, generationID)
}

func containerImageIdentityCutoverCacheLegacyRow(
	factID string,
	scopeID string,
	generationID string,
) factwrite.Row {
	row := containerImageIdentityLegacyLiveRow(factID, 0, false)
	row.ScopeID = scopeID
	row.GenerationID = generationID
	return row
}

func cleanupContainerImageIdentityCutoverCacheScope(
	t *testing.T,
	db *sql.DB,
	scopeID string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(
		ctx,
		"DELETE FROM ingestion_scopes WHERE scope_id = $1",
		scopeID,
	); err != nil {
		t.Errorf("clean cache-test scope %s: %v", scopeID, err)
	}
}
