// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// strandedPendingProjectorSeedSQL reproduces the #4727 wedge state that a
// retryable canonical-write failure under bootstrap-index leaves behind: the
// generation is 'pending' (never activated), and its projector work item is
// 'retrying' with a now-past visible_at and a dead lease owner (bootstrap-index
// exited). Claimability is filtered on stage='projector' (domain='source_local'
// is set for fidelity to the real row but is inert to the claim). In
// docker-compose the only continuous claimer besides bootstrap (the ingester) is
// gated on bootstrap-index success, so after a bootstrap failure this
// perfectly-claimable item had no claimer forever.
const strandedPendingProjectorSeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-stranded', 'repository', 'github', 'acme/stranded', 'git',
    'acme/stranded', now(), now() - interval '2 hours', 'pending', NULL
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-stranded', 'scope-stranded', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'pending', NULL
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, conflict_domain,
    payload, created_at, updated_at
) VALUES (
    'projector_scope-stranded_gen-stranded', 'scope-stranded', 'gen-stranded',
    'projector', 'source_local', 'retrying',
    1, NULL, NULL, now() - interval '1 hour', 'scope',
    '{}'::jsonb, now() - interval '2 hours', now() - interval '1 hour'
);
`

// TestProjectorStrandedRetryRecovery is the #4727 failing→green regression.
//
// FAILING side: the generation-liveness sweep cannot recover this state — its
// three gates each exclude it (active-only; own-outstanding-intents, of which a
// never-activated generation has none; retrying-item-is-in-flight). This pins
// that no existing recovery mechanism drains the stranded item.
//
// GREEN side: an ungated projector-owner Claim (the E1 fix: a standalone
// projector service not gated on bootstrap-index) claims the stranded item and
// Ack activates its generation. This is exactly what the added docker-compose
// projector service does, restoring convergence.
func TestProjectorStrandedRetryRecovery(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN to run the #4727 stranded-retry recovery proof")
	}
	db := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, db, strandedPendingProjectorSeedSQL)
	ctx := context.Background()
	now := time.Now().UTC()

	// FAILING side: the liveness sweep does not recover the stranded-pending item.
	store := NewGenerationLivenessStore(SQLDB{DB: db})
	res, err := store.RecoverWedgedGenerations(ctx, GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 5,
		BatchLimit:         100,
	}, now)
	if err != nil {
		t.Fatalf("RecoverWedgedGenerations: %v", err)
	}
	if res.Recovered != 0 {
		t.Fatalf("liveness sweep recovered %d generations; the stranded-pending #4727 state is outside its remit (active-only / own-intents / in-flight gates)", res.Recovered)
	}

	// GREEN side: an ungated projector-owner claim (distinct lease owner) drains it.
	queue := NewProjectorQueue(SQLDB{DB: db}, "projector", time.Minute)
	work, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("projector Claim: %v", err)
	}
	if !ok {
		t.Fatalf("projector Claim found no work: the stranded #4727 item was not claimable by an ungated projector")
	}
	if work.Scope.ScopeID != "scope-stranded" || work.Generation.GenerationID != "gen-stranded" {
		t.Fatalf("projector claimed the wrong work: scope=%q gen=%q, want scope-stranded/gen-stranded", work.Scope.ScopeID, work.Generation.GenerationID)
	}

	// Ack (successful re-projection) activates the generation and points the
	// scope at it — the state whose absence wedged cross-scope readiness.
	if err := queue.Ack(ctx, work, projector.Result{}); err != nil {
		t.Fatalf("projector Ack: %v", err)
	}
	var genStatus string
	var activatedAt *time.Time
	if err := db.QueryRowContext(ctx, "SELECT status, activated_at FROM scope_generations WHERE generation_id = 'gen-stranded'").Scan(&genStatus, &activatedAt); err != nil {
		t.Fatalf("read generation after Ack: %v", err)
	}
	if genStatus != "active" || activatedAt == nil {
		t.Fatalf("generation status=%q activated_at=%v after projector Ack; want active + set", genStatus, activatedAt)
	}
	var activeGen *string
	if err := db.QueryRowContext(ctx, "SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = 'scope-stranded'").Scan(&activeGen); err != nil {
		t.Fatalf("read scope after Ack: %v", err)
	}
	if activeGen == nil || *activeGen != "gen-stranded" {
		t.Fatalf("scope active_generation_id=%v after Ack; want gen-stranded", activeGen)
	}
}

// TestProjectorStrandedRetryRecoveryLeaveLiveLease is the negative case: a
// source_local item still under a LIVE lease (claim_until in the future) is
// actively being projected and must NOT be claimed out from under its owner.
func TestProjectorStrandedRetryRecoveryLeaveLiveLease(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN to run the #4727 live-lease negative proof")
	}
	db := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, db, liveLeaseProjectorSeedSQL)
	ctx := context.Background()

	queue := NewProjectorQueue(SQLDB{DB: db}, "projector", time.Minute)
	_, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("projector Claim: %v", err)
	}
	if ok {
		t.Fatalf("projector claimed an item under a live lease; an actively-projecting generation must not be re-driven")
	}
}

// liveLeaseProjectorSeedSQL is the negative sibling: the source_local item is
// 'running' under a lease that has NOT expired (claim_until in the future),
// i.e. bootstrap-index is actively projecting it right now.
const liveLeaseProjectorSeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-live', 'repository', 'github', 'acme/live', 'git',
    'acme/live', now(), now() - interval '2 hours', 'pending', NULL
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-live', 'scope-live', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'pending', NULL
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, conflict_domain,
    payload, created_at, updated_at
) VALUES (
    'projector_scope-live_gen-live', 'scope-live', 'gen-live',
    'projector', 'source_local', 'running',
    0, 'bootstrap-index', now() + interval '30 seconds', now() - interval '1 hour', 'scope',
    '{}'::jsonb, now() - interval '2 hours', now() - interval '1 minute'
);
`
