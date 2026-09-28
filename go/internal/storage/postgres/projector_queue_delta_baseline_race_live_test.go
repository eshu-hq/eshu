// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestProjectorAckFencesDeltaAgainstActivationWhileWaiting is the binding
// #7319 case. It uses only symbols that exist on main, so the same file runs
// RED there.
//
// Active is G_A (commit A). G_B (a full generation at commit B) and G_D (a
// delta diffed from A to D) are both claimed. Session 1 runs G_B's Ack
// statements and holds its transaction open. Session 2 is the production Ack
// of G_D; it blocks on the scope row. Session 1 commits, so G_B is active when
// session 2 resumes. G_D's baseline A is no longer the active commit, so its
// overlay would be applied on top of B: Ack must refuse it and leave G_B
// active. On main, G_D activates.
func TestProjectorAckFencesDeltaAgainstActivationWhileWaiting(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	projectorDB := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, projectorDB, `
ALTER TABLE scope_generations ADD COLUMN IF NOT EXISTS delta_baseline_commit_sha TEXT NULL;
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-7319', 'repository', 'github', 'proof/7319', 'git',
          'proof/7319', now(), now(), 'active', 'gen-a');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
    activated_at, source_commit_sha, is_delta, delta_baseline_commit_sha
) VALUES
    ('gen-a', 'scope-7319', 'snapshot', now() - interval '3 hours', now() - interval '3 hours',
     'active', now() - interval '3 hours', 'A', false, NULL),
    ('gen-b', 'scope-7319', 'snapshot', now() - interval '2 hours', now() - interval '2 hours',
     'pending', NULL, 'B', false, NULL),
    ('gen-d', 'scope-7319', 'snapshot', now() - interval '1 hour', now() - interval '1 hour',
     'pending', NULL, 'D', true, 'A');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES
    ('projector_scope-7319_gen-b', 'scope-7319', 'gen-b', 'projector', 'source_local', 'running', 1,
     'worker-b', now() + interval '5 minutes', now(), '{}'::jsonb, now(), now()),
    ('projector_scope-7319_gen-d', 'scope-7319', 'gen-d', 'projector', 'source_local', 'running', 1,
     'worker-d', now() + interval '5 minutes', now(), '{}'::jsonb, now(), now());
`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var searchPath string
	if err := projectorDB.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read proof search_path: %v", err)
	}
	session1 := openLivenessProofDB(t, dsn)
	if _, err := session1.ExecContext(ctx, "SET search_path TO "+searchPath); err != nil {
		t.Fatalf("set session 1 search_path: %v", err)
	}
	tx1, err := session1.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin session 1: %v", err)
	}
	defer func() { _ = tx1.Rollback() }()
	now := time.Now().UTC()
	for _, step := range []struct {
		query string
		args  []any
	}{
		{updateProjectorScopeGenerationQuery, []any{now, "scope-7319", "gen-b"}},
		{ackProjectorWorkItemQuery, []any{now, "scope-7319", "gen-b", "worker-b", 1}},
		{supersedeProjectorObsoleteGenerationsQuery, []any{now, "scope-7319", "gen-b"}},
		{supersedeProjectorActiveGenerationQuery, []any{now, "scope-7319", "gen-b"}},
		{activateProjectorGenerationQuery, []any{now, "scope-7319", "gen-b"}},
	} {
		if _, err := tx1.ExecContext(ctx, step.query, step.args...); err != nil {
			t.Fatalf("session 1 ack statement: %v", err)
		}
	}
	var xid string
	if err := tx1.QueryRowContext(ctx, "SELECT pg_current_xact_id()::text").Scan(&xid); err != nil {
		t.Fatalf("read session 1 xid: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: projectorDB}, "worker-d", time.Minute)
	queue.AckScopeLockTimeout = 4 * time.Second
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-7319"},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-d"},
		AttemptCount: 1,
	}
	ackErr := make(chan error, 1)
	go func() { ackErr <- queue.Ack(ctx, work, runtime.Result{}) }()
	waitForUngrantedLock(ctx, t, openLivenessProofDB(t, dsn), xid)
	if err := tx1.Commit(); err != nil {
		t.Fatalf("commit session 1: %v", err)
	}

	err = <-ackErr
	var generationD, generationB, pointer, workStatus, failureClass string
	if scanErr := projectorDB.QueryRowContext(context.Background(), `
SELECT
    (SELECT status FROM scope_generations WHERE generation_id = 'gen-d'),
    (SELECT status FROM scope_generations WHERE generation_id = 'gen-b'),
    (SELECT COALESCE(active_generation_id, '') FROM ingestion_scopes WHERE scope_id = 'scope-7319'),
    (SELECT status FROM fact_work_items WHERE work_item_id = 'projector_scope-7319_gen-d'),
    (SELECT COALESCE(failure_class, '') FROM fact_work_items WHERE work_item_id = 'projector_scope-7319_gen-d')`,
	).Scan(&generationD, &generationB, &pointer, &workStatus, &failureClass); scanErr != nil {
		t.Fatalf("read race outcome: %v", scanErr)
	}
	t.Logf("race outcome: ack_err=%v gen-d=%s gen-b=%s pointer=%s work=%s class=%s",
		err, generationD, generationB, pointer, workStatus, failureClass)
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack(gen-d) after gen-b activated = %v, want ErrWorkSuperseded", err)
	}
	if generationD != "superseded" || generationB != "active" || pointer != "gen-b" ||
		workStatus != "superseded" || failureClass != "projector_delta_baseline_mismatch_after_projection" {
		t.Fatalf("gen-d=%s gen-b=%s pointer=%s work=%s class=%s; want superseded, active, gen-b, superseded, "+
			"projector_delta_baseline_mismatch_after_projection", generationD, generationB, pointer, workStatus, failureClass)
	}
	if !strings.Contains(err.Error(), "projector_delta_baseline_mismatch_after_projection") {
		t.Fatalf("Ack error %q does not name the refusal class", err)
	}
}
