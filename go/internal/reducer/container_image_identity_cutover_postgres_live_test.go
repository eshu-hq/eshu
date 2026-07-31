// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestPostgresContainerImageIdentityLegacyWriterFailsClosedAboveReadCommitted(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	const factID = "reducer_container_image_identity:5854-repeatable-read"
	if _, err := db.ExecContext(ctx, `DELETE FROM fact_records WHERE fact_id = $1`, factID); err != nil {
		t.Fatalf("clean repeatable-read legacy row: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(
			context.Background(),
			`DELETE FROM fact_records WHERE fact_id = $1`,
			factID,
		); err != nil {
			t.Errorf("clean repeatable-read legacy row: %v", err)
		}
	})

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatalf("begin repeatable-read legacy transaction: %v", err)
	}
	err = reducerBatchInsertFacts(
		ctx,
		tx,
		[]reducerFactRow{containerImageIdentityLegacyLiveRow(factID, 0, false)},
	)
	if err == nil || !strings.Contains(
		err.Error(),
		"legacy container image identity writes require read committed isolation",
	) {
		t.Fatalf("repeatable-read legacy insert error = %v, want fail-closed isolation error", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("roll back repeatable-read legacy transaction: %v", err)
	}

	markerTx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatalf("begin repeatable-read marker transaction: %v", err)
	}
	defer func() { _ = markerTx.Rollback() }()
	err = execContainerImageIdentityCutoverFence(
		ctx,
		markerTx,
		containerImageIdentityLiveScope,
		containerImageIdentityLiveGeneration,
		containerImageIdentityLiveWorkItemID(containerImageIdentityLiveGeneration),
		1,
	)
	if err == nil || !strings.Contains(
		err.Error(),
		"legacy container image identity writes require read committed isolation",
	) {
		t.Fatalf("repeatable-read marker insert error = %v, want fail-closed isolation error", err)
	}
}

func TestPostgresContainerImageIdentityCutoverFenceKeepsOtherScopeConcurrent(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	const (
		otherScope      = "repository:5854-live-other"
		otherGeneration = "generation:5854-live-other"
		otherFactID     = "reducer_container_image_identity:5854-live-other"
	)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    $1, 'repository', 'git', 'synthetic-5854-other', 'reducer',
    'synthetic-5854-other', clock_timestamp(), clock_timestamp(), 'active'
)
ON CONFLICT (scope_id) DO NOTHING;
`, otherScope); err != nil {
		t.Fatalf("seed other cutover scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    $2, $1, 'synthetic', FALSE, clock_timestamp(), clock_timestamp(), 'active'
)
ON CONFLICT (generation_id) DO NOTHING;
`, otherScope, otherGeneration); err != nil {
		t.Fatalf("seed other cutover generation: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM fact_records WHERE fact_id = $1`,
		otherFactID,
	); err != nil {
		t.Fatalf("clean other-scope identity row: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM container_image_identity_cutovers
		 WHERE scope_id = $1 AND generation_id = $2`,
		otherScope,
		otherGeneration,
	); err != nil {
		t.Fatalf("clean other-scope cutover row: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(
			cleanupCtx,
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
			otherScope,
		); err != nil {
			t.Errorf("clean other cutover scope: %v", err)
		}
	})

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin target cutover transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := execContainerImageIdentityCutoverFence(
		ctx,
		tx,
		containerImageIdentityLiveScope,
		containerImageIdentityLiveGeneration,
		containerImageIdentityLiveWorkItemID(containerImageIdentityLiveGeneration),
		1,
	); err != nil {
		t.Fatalf("hold target cutover fence: %v", err)
	}

	row := containerImageIdentityLegacyLiveRow(otherFactID, 0, false)
	row.ScopeID = otherScope
	row.GenerationID = otherGeneration
	done := make(chan error, 1)
	go func() {
		done <- reducerBatchInsertFacts(ctx, db, []reducerFactRow{row})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write legacy identity in other scope: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("other-scope legacy write blocked on target scope cutover")
	}

	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		1,
		otherFactID,
	)
}
