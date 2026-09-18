// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestProjectorHeartbeatRenewsLeaseWhileIngestionHoldsScope proves that an
// ingestion commit holding the scope row cannot stall projector heartbeats.
// Ingestion keeps that lock while it streams facts, which can outlast the
// projector lease; a heartbeat waiting on it would let another attempt reclaim
// the work. Heartbeat must renew the lease immediately and defer supersession
// until the scope is free.
func TestProjectorHeartbeatRenewsLeaseWhileIngestionHoldsScope(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	projectorDB := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, projectorDB, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-hb', 'repository', 'github', 'proof/hb', 'git',
          'proof/hb', now(), now(), 'active', 'gen-old');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES ('gen-old', 'scope-hb', 'push', now(), now(), 'active', now()),
         ('gen-new', 'scope-hb', 'push', now() + interval '1 minute',
          now() + interval '1 minute', 'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-hb_gen-old', 'scope-hb', 'gen-old',
          'projector', 'source_local', 'running', 1, 'proof-worker',
          now() + interval '5 seconds', now(), '{}'::jsonb, now(), now());
`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		"UPDATE ingestion_scopes SET observed_at = observed_at WHERE scope_id = 'scope-hb'",
	); err != nil {
		t.Fatalf("lock scope as ingestion: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: projectorDB}, "proof-worker", time.Minute)
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-hb"},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-old"},
		AttemptCount: 1,
	}

	heartbeatCtx, cancelHeartbeat := context.WithTimeout(ctx, time.Second)
	started := time.Now()
	err = queue.Heartbeat(heartbeatCtx, work)
	cancelHeartbeat()
	if err != nil {
		t.Fatalf("Heartbeat while ingestion holds the scope = %v after %s, want lease renewal", err, time.Since(started))
	}
	var leaseSeconds float64
	var workStatus, generationStatus string
	if err := projectorDB.QueryRowContext(ctx, `
SELECT EXTRACT(EPOCH FROM work.claim_until - now())::float8, work.status, generation.status
FROM fact_work_items AS work
JOIN scope_generations AS generation ON generation.generation_id = work.generation_id
WHERE work.work_item_id = 'projector_scope-hb_gen-old'`).Scan(&leaseSeconds, &workStatus, &generationStatus); err != nil {
		t.Fatalf("read renewed lease: %v", err)
	}
	if leaseSeconds < 30 || workStatus != "running" || generationStatus != "active" {
		t.Fatalf("after heartbeat: lease=%.1fs work=%s generation=%s, want lease near 60s, running, active",
			leaseSeconds, workStatus, generationStatus)
	}

	// The same-generation enqueue conflicts on the deterministic work row that
	// Heartbeat just renewed. It must finish while ingestion still holds the
	// scope, which is the interleaving that previously deadlocked.
	ingestQueue := NewProjectorQueue(SQLTx{Tx: ingestTx}, "proof-worker", time.Minute)
	if err := ingestQueue.Enqueue(ctx, "scope-hb", "gen-old"); err != nil {
		t.Fatalf("same-generation ingest enqueue after heartbeat: %v", err)
	}
	if err := ingestTx.Commit(); err != nil {
		t.Fatalf("commit ingest tx: %v", err)
	}
	if err := queue.Heartbeat(ctx, work); !errors.Is(err, projector.ErrWorkSuperseded) {
		t.Fatalf("Heartbeat after ingestion released the scope = %v, want ErrWorkSuperseded", err)
	}
}
