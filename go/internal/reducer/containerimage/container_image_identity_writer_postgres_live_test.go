// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
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
	write := containerImageIdentityLiveWrite(base, imageRef, reducercontract.ContainerImageIdentityTagResolved, 1)
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	cutoverLookup := containerImageIdentityAtomicLiveCutoverLookup{db: db}
	writer := PostgresContainerImageIdentityWriter{
		DB:            db,
		Beginner:      &containerImageIdentityAtomicLiveBeginner{db: db},
		CutoverLookup: cutoverLookup,
		ClaimedExecer: containerImageIdentityAtomicLiveClaimedExecer{db: db},
	}
	factID := containerImageIdentityFactID(write, write.Decisions[0])
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	})

	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, write); err != nil {
		t.Fatalf("seed canonical identity: %v", err)
	}

	tombstone := containerImageIdentityLiveWrite(
		base.Add(2*time.Second),
		imageRef,
		reducercontract.ContainerImageIdentityUnresolved,
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
		reducercontract.ContainerImageIdentityExactDigest,
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
		reducercontract.ContainerImageIdentityExactDigest,
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
	if err := factwrite.BatchInsertFacts(
		ctx,
		staleTx,
		[]factwrite.Row{containerImageIdentityLiveRow(matchingFactID, 5, false)},
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
		freshDone <- factwrite.BatchInsertFacts(
			ctx,
			freshTx,
			[]factwrite.Row{containerImageIdentityLiveRow(matchingFactID, 9, true)},
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
	if err := factwrite.BatchInsertFacts(
		ctx,
		distinctTx,
		[]factwrite.Row{containerImageIdentityLiveRow(distinctFactID, 7, false)},
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
	seedContainerImageIdentityLiveWorkItem(
		t,
		ctx,
		db,
		containerImageIdentityLiveScope,
		containerImageIdentityLiveGeneration,
	)
}

func seedContainerImageIdentityLiveWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
DELETE FROM fact_work_items
WHERE scope_id = $1
  AND generation_id = $2
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
`, scopeID, generationID); err != nil {
		t.Fatalf("reset live container image identity work item: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
	work_item_id, scope_id, generation_id, stage, domain,
	conflict_domain, conflict_key, status, attempt_count,
	container_image_identity_claim_epoch,
	lease_owner, claim_until, payload, created_at, updated_at
) VALUES (
	$3, $1, $2, 'reducer', 'container_image_identity',
	'intent', $3, 'running', 1,
	1,
	'reducer', clock_timestamp() + interval '5 minutes',
    jsonb_build_object(
        'entity_key', $3::text,
        'reason', 'synthetic container image identity live proof',
        'source_system', 'git'
    ),
    clock_timestamp(), clock_timestamp()
)
`, scopeID, generationID, containerImageIdentityLiveWorkItemID(generationID)); err != nil {
		t.Fatalf("seed live container image identity work item: %v", err)
	}
}

func containerImageIdentityLiveWorkItemID(generationID string) string {
	return "work-5854-container-image-identity:" + generationID
}

func containerImageIdentityLiveWrite(
	evidenceAsOf time.Time,
	imageRef string,
	outcome reducercontract.ContainerImageIdentityOutcome,
	canonicalWrites int,
) ContainerImageIdentityWrite {
	return ContainerImageIdentityWrite{
		IntentID:     containerImageIdentityLiveWorkItemID(containerImageIdentityLiveGeneration),
		ClaimEpoch:   1,
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
) factwrite.Row {
	now := time.Date(2026, time.July, 29, 15, 1, 0, 0, time.UTC)
	return factwrite.Row{
		FactID:           factID,
		ScopeID:          containerImageIdentityLiveScope,
		GenerationID:     containerImageIdentityLiveGeneration,
		FactKind:         reducercontract.ContainerImageIdentityFactKind,
		StableFactKey:    factID,
		CollectorKind:    "reducer",
		SourceConfidence: "inferred",
		SourceSystem:     "git",
		SourceFactKey:    "intent-5854-live",
		ObservedAt:       now,
		IngestedAt:       now,
		IsTombstone:      tombstone,
		Payload:          `{"identity_format":"image_ref_v2","image_ref":"registry.example.com/team/api:prod"}`,
		FencingToken:     token,
	}
}

func containerImageIdentityLegacyLiveRow(
	factID string,
	token int64,
	tombstone bool,
) factwrite.Row {
	row := containerImageIdentityLiveRow(factID, token, tombstone)
	row.Payload = `{"image_ref":"registry.example.com/team/api:prod"}`
	return row
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
