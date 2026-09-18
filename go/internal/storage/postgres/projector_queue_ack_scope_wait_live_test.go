// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestProjectorAckDefersWhileIngestionHoldsScope proves Ack cannot wait out
// its caller's ack budget behind an ingestion commit that holds the scope row
// while streaming facts. Ack must give up quickly with ErrWorkAckDeferred and
// change nothing, so the attempt keeps its claim and can retry; once ingestion
// commits, the same Ack publishes the generation.
func TestProjectorAckDefersWhileIngestionHoldsScope(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	projectorDB := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, projectorDB, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-ack', 'repository', 'github', 'proof/ack', 'git',
          'proof/ack', now(), now(), 'active', 'gen-old');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES ('gen-old', 'scope-ack', 'push', now(), now(), 'active', now()),
         ('gen-new', 'scope-ack', 'push', now() + interval '1 minute',
          now() + interval '1 minute', 'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-ack_gen-new', 'scope-ack', 'gen-new',
          'projector', 'source_local', 'running', 1, 'proof-worker',
          now() + interval '1 minute', now(), '{}'::jsonb, now(), now());
`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var searchPath string
	if err := projectorDB.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read proof search_path: %v", err)
	}
	ingestDB := openLivenessProofDB(t, dsn)
	if _, err := ingestDB.ExecContext(ctx, "SET search_path TO "+searchPath); err != nil {
		t.Fatalf("set ingest search_path: %v", err)
	}
	ingestTx, err := ingestDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin ingest tx: %v", err)
	}
	defer func() { _ = ingestTx.Rollback() }()
	// Same row lock as the ingestion scope upsert (ON CONFLICT DO UPDATE).
	if _, err := ingestTx.ExecContext(ctx,
		"UPDATE ingestion_scopes SET observed_at = observed_at WHERE scope_id = 'scope-ack'",
	); err != nil {
		t.Fatalf("lock scope as ingestion: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: projectorDB}, "proof-worker", time.Minute)
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-ack"},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-new"},
		AttemptCount: 1,
	}
	// The service gives Ack a 5 s budget; Ack must return well inside it.
	ackCtx, cancelAck := context.WithTimeout(ctx, 5*time.Second)
	started := time.Now()
	err = queue.Ack(ackCtx, work, projector.Result{})
	elapsed := time.Since(started)
	cancelAck()
	if !errors.Is(err, projector.ErrWorkAckDeferred) {
		t.Fatalf("Ack while ingestion holds the scope = %v after %s, want ErrWorkAckDeferred", err, elapsed)
	}
	if elapsed >= 4*time.Second {
		t.Fatalf("Ack deferral took %s, want well under the 5 s ack budget", elapsed)
	}
	assertAckScopeState(t, projectorDB, "running", "active", "pending", "gen-old")

	if err := ingestTx.Commit(); err != nil {
		t.Fatalf("commit ingest tx: %v", err)
	}
	if err := queue.Ack(ctx, work, projector.Result{}); err != nil {
		t.Fatalf("Ack after ingestion released the scope: %v", err)
	}
	assertAckScopeState(t, projectorDB, "succeeded", "superseded", "active", "gen-new")
}

func assertAckScopeState(t *testing.T, db *sql.DB, wantWork, wantOld, wantNew, wantPointer string) {
	t.Helper()
	var work, oldGen, newGen, pointer string
	if err := db.QueryRowContext(context.Background(), `
SELECT
    (SELECT status FROM fact_work_items WHERE work_item_id = 'projector_scope-ack_gen-new'),
    (SELECT status FROM scope_generations WHERE generation_id = 'gen-old'),
    (SELECT status FROM scope_generations WHERE generation_id = 'gen-new'),
    (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = 'scope-ack')`,
	).Scan(&work, &oldGen, &newGen, &pointer); err != nil {
		t.Fatalf("read ack state: %v", err)
	}
	if work != wantWork || oldGen != wantOld || newGen != wantNew || pointer != wantPointer {
		t.Fatalf("state work=%s old=%s new=%s pointer=%s, want work=%s old=%s new=%s pointer=%s",
			work, oldGen, newGen, pointer, wantWork, wantOld, wantNew, wantPointer)
	}
}
