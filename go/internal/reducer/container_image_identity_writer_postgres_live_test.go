// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	containerImageIdentityLiveScope      = "repository:5854-live"
	containerImageIdentityLiveGeneration = "generation:5854-live"
)

func TestPostgresContainerImageIdentityTombstoneFencePreventsStaleResurrection(t *testing.T) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx := context.Background()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	imageRef := "registry.example.com/team/api:prod"
	base := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	writer := PostgresContainerImageIdentityWriter{DB: db}
	write := containerImageIdentityLiveWrite(base, imageRef, ContainerImageIdentityTagResolved, 1)
	factID := containerImageIdentityFactID(write, write.Decisions[0])
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM fact_records WHERE fact_id = $1`, factID); err != nil {
			t.Errorf("clean live identity fact: %v", err)
		}
	})

	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, write); err != nil {
		t.Fatalf("seed canonical identity: %v", err)
	}

	tombstone := containerImageIdentityLiveWrite(
		base.Add(2*time.Second),
		imageRef,
		ContainerImageIdentityUnresolved,
		0,
	)
	tombstone.TombstoneDecisions = append(
		tombstone.TombstoneDecisions,
		tombstone.Decisions[0],
	)
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, tombstone); err != nil {
		t.Fatalf("write fresh tombstone: %v", err)
	}

	stale := containerImageIdentityLiveWrite(
		base.Add(time.Second),
		imageRef,
		ContainerImageIdentityExactDigest,
		1,
	)
	stale.Decisions[0].Digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, stale); err != nil {
		t.Fatalf("write stale canonical identity: %v", err)
	}
	assertContainerImageIdentityLiveRow(
		t,
		ctx,
		db,
		factID,
		true,
		tombstone.EvidenceAsOf.UnixMicro(),
	)

	fresh := containerImageIdentityLiveWrite(
		base.Add(3*time.Second),
		imageRef,
		ContainerImageIdentityExactDigest,
		1,
	)
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, fresh); err != nil {
		t.Fatalf("revive identity with fresher evidence: %v", err)
	}
	assertContainerImageIdentityLiveRow(
		t,
		ctx,
		db,
		factID,
		false,
		fresh.EvidenceAsOf.UnixMicro(),
	)
}

func TestPostgresContainerImageIdentityFactFenceSerializesOnlyMatchingLogicalKey(t *testing.T) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx := context.Background()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	const (
		matchingFactID = "reducer_container_image_identity:5854-live-matching"
		distinctFactID = "reducer_container_image_identity:5854-live-distinct"
	)
	t.Cleanup(func() {
		if _, err := db.ExecContext(
			context.Background(),
			`DELETE FROM fact_records WHERE fact_id = ANY($1::text[])`,
			[]string{matchingFactID, distinctFactID},
		); err != nil {
			t.Errorf("clean live concurrency facts: %v", err)
		}
	})

	staleTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin stale transaction: %v", err)
	}
	defer func() { _ = staleTx.Rollback() }()
	if err := reducerBatchInsertFacts(
		ctx,
		staleTx,
		[]reducerFactRow{containerImageIdentityLiveRow(matchingFactID, 5, false)},
	); err != nil {
		t.Fatalf("insert uncommitted stale row: %v", err)
	}

	freshTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin fresh transaction: %v", err)
	}
	defer func() { _ = freshTx.Rollback() }()
	freshDone := make(chan error, 1)
	go func() {
		freshDone <- reducerBatchInsertFacts(
			ctx,
			freshTx,
			[]reducerFactRow{containerImageIdentityLiveRow(matchingFactID, 9, true)},
		)
	}()

	select {
	case err := <-freshDone:
		t.Fatalf("matching-key upsert returned before conflicting transaction committed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	distinctTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin distinct-key transaction: %v", err)
	}
	if err := reducerBatchInsertFacts(
		ctx,
		distinctTx,
		[]reducerFactRow{containerImageIdentityLiveRow(distinctFactID, 7, false)},
	); err != nil {
		_ = distinctTx.Rollback()
		t.Fatalf("insert distinct key while matching key is locked: %v", err)
	}
	if err := distinctTx.Commit(); err != nil {
		t.Fatalf("commit distinct-key transaction: %v", err)
	}

	if err := staleTx.Commit(); err != nil {
		t.Fatalf("commit stale transaction: %v", err)
	}
	select {
	case err := <-freshDone:
		if err != nil {
			t.Fatalf("fresh upsert after conflict release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fresh matching-key upsert did not resume after conflict release")
	}
	if err := freshTx.Commit(); err != nil {
		t.Fatalf("commit fresh transaction: %v", err)
	}

	assertContainerImageIdentityLiveRow(t, ctx, db, matchingFactID, true, 9)
	assertContainerImageIdentityLiveRow(t, ctx, db, distinctFactID, false, 7)
}

func openContainerImageIdentityLivePostgres(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set; skipping Postgres integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping Postgres: %v", err)
	}
	return db
}

func seedContainerImageIdentityLiveParents(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	now := time.Date(2026, time.July, 29, 14, 59, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
		VALUES ($1, 'repository', 'git', 'synthetic-5854', 'reducer', 'synthetic-5854', $2, $2, 'active')
		ON CONFLICT (scope_id) DO NOTHING`, containerImageIdentityLiveScope, now); err != nil {
		t.Fatalf("seed live ingestion scope: %v", err)
	}
	if _, err := db.ExecContext(
		ctx, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status)
		VALUES ($1, $2, 'synthetic', false, $3, $3, 'active')
		ON CONFLICT (generation_id) DO NOTHING`,
		containerImageIdentityLiveGeneration,
		containerImageIdentityLiveScope,
		now,
	); err != nil {
		t.Fatalf("seed live scope generation: %v", err)
	}
}

func containerImageIdentityLiveWrite(
	evidenceAsOf time.Time,
	imageRef string,
	outcome ContainerImageIdentityOutcome,
	canonicalWrites int,
) ContainerImageIdentityWrite {
	return ContainerImageIdentityWrite{
		IntentID:     "intent-5854-live",
		ScopeID:      containerImageIdentityLiveScope,
		GenerationID: containerImageIdentityLiveGeneration,
		SourceSystem: "git",
		Cause:        "synthetic live proof",
		EvidenceAsOf: evidenceAsOf,
		Decisions: []ContainerImageIdentityDecision{{
			ImageRef:        imageRef,
			Digest:          retirementTestDigest,
			RepositoryID:    retirementTestRepositoryID,
			Outcome:         outcome,
			CanonicalWrites: canonicalWrites,
		}},
	}
}

func containerImageIdentityLiveRow(
	factID string,
	token int64,
	tombstone bool,
) reducerFactRow {
	now := time.Date(2026, time.July, 29, 15, 1, 0, 0, time.UTC)
	return reducerFactRow{
		FactID:           factID,
		ScopeID:          containerImageIdentityLiveScope,
		GenerationID:     containerImageIdentityLiveGeneration,
		FactKind:         containerImageIdentityFactKind,
		StableFactKey:    factID,
		CollectorKind:    "reducer",
		SourceConfidence: "inferred",
		SourceSystem:     "git",
		SourceFactKey:    "intent-5854-live",
		ObservedAt:       now,
		IngestedAt:       now,
		IsTombstone:      tombstone,
		Payload:          `{"image_ref":"registry.example.com/team/api:prod"}`,
		FencingToken:     token,
	}
}

func assertContainerImageIdentityLiveRow(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	factID string,
	wantTombstone bool,
	wantToken int64,
) {
	t.Helper()

	var (
		gotTombstone bool
		gotToken     int64
	)
	if err := db.QueryRowContext(
		ctx,
		`SELECT is_tombstone, fencing_token FROM fact_records WHERE fact_id = $1`,
		factID,
	).Scan(&gotTombstone, &gotToken); err != nil {
		t.Fatalf("read live fact %q: %v", factID, err)
	}
	if gotTombstone != wantTombstone || gotToken != wantToken {
		t.Fatalf(
			"live fact %q = tombstone %t token %d, want tombstone %t token %d",
			factID,
			gotTombstone,
			gotToken,
			wantTombstone,
			wantToken,
		)
	}
}
