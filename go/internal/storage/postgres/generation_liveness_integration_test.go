package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestGenerationLivenessIntegration exercises RecoverWedgedGenerations and
// CountActiveGenerationsByAge against a real Postgres instance seeded with
// scope_generations and fact_work_items fixtures. Set
// ESHU_GENERATION_LIVENESS_PROOF_DSN to a Postgres DSN to run the suite;
// the test is skipped when the env var is absent so the normal unit gate is
// unaffected.
//
// Scenarios covered:
//
//  1. Wedged re-drive: a generation that has aged past the activation deadline
//     and has outstanding shared_projection_intents is re-enqueued exactly once
//     with liveness_recovery_attempts = 1. A second sweep past the budget ceiling
//     (MaxRecoverAttempts = 1) does NOT re-enqueue — the budget gate and LEAST
//     cap prevent a poison scope from looping.
//
//  2. Orphaned supersede: an older active generation for a scope that already has
//     a newer active generation is retired to 'superseded'; the newest generation
//     is untouched. A repeat sweep is a no-op (idempotent).
//
//  3. No-op safety: a fresh active generation within the activation deadline is
//     never re-driven. A scope whose pending generation is newer than the active
//     one is also left alone (the projector supersede path owns that case).
//
//  4. CountActiveGenerationsByAge returns correct fresh/aging/stuck counts. The
//     stuck bucket requires an outstanding shared_projection_intents row; it does
//     not filter for newer siblings, so gen-pending-active is also stuck. The
//     aging bucket contains aged-but-drained generations (gen-aging, gen-orphaned-*).
func TestGenerationLivenessIntegration(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN to run the generation liveness integration proof")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schemaName := fmt.Sprintf("liveness_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, generationLivenessProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	if _, err := db.ExecContext(ctx, generationLivenessProofSeedSQL); err != nil {
		t.Fatalf("seed proof data: %v", err)
	}

	store := NewGenerationLivenessStore(SQLDB{DB: db})

	// Policy: 30-minute activation deadline, budget of 1 re-drive attempt.
	policy := GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 1,
		BatchLimit:         100,
	}
	now := time.Now().UTC()

	// ---------------------------------------------------------------------------
	// Scenario 4: CountActiveGenerationsByAge before any recovery sweep.
	// ---------------------------------------------------------------------------
	t.Run("CountActiveGenerationsByAge", func(t *testing.T) {
		counts, err := store.CountActiveGenerationsByAge(ctx, policy, now)
		if err != nil {
			t.Fatalf("CountActiveGenerationsByAge() error = %v", err)
		}
		// Stuck bucket: generations past the full deadline with outstanding
		// shared_projection_intents (completed_at IS NULL).
		//   gen-wedged: activated 2h ago, outstanding intent → stuck.
		//   gen-pending-active: activated 2h ago, outstanding intent → stuck.
		//     (CountActiveGenerationsByAge does not filter for newer siblings —
		//      only RecoverWedgedGenerations excludes them via NOT EXISTS.)
		if got, want := counts["stuck"], int64(2); got != want {
			t.Fatalf("stuck count = %d, want %d", got, want)
		}
		// Fresh bucket: generations with no activated_at or activated within half
		// the activation deadline (15 min).
		//   gen-fresh: activated 5 min ago → fresh.
		if got, want := counts["fresh"], int64(1); got != want {
			t.Fatalf("fresh count = %d, want %d", got, want)
		}
		// Aging bucket: generations past half the deadline but not stuck.
		//   gen-aging: activated 20 min ago, all intents completed → aging.
		//   gen-orphaned-old: activated 3h ago, no intents → aging.
		//   gen-orphaned-new: activated 1h ago, no intents → aging.
		if got, want := counts["aging"], int64(3); got != want {
			t.Fatalf("aging count = %d, want %d", got, want)
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario 2 (run before scenario 1): orphaned supersede.
	// scope-orphaned has two active generations: gen-orphaned-old and
	// gen-orphaned-new. The older one must be retired; the newer one must be
	// untouched. A second sweep is a no-op.
	// ---------------------------------------------------------------------------
	t.Run("OrphanedSupersede", func(t *testing.T) {
		result, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("RecoverWedgedGenerations() error = %v", err)
		}
		// Exactly one orphaned active generation retired.
		if got, want := result.Superseded, 1; got != want {
			t.Fatalf("Superseded = %d, want %d", got, want)
		}
		if len(result.SupersededScopeIDs) != 1 || result.SupersededScopeIDs[0] != "scope-orphaned" {
			t.Fatalf("SupersededScopeIDs = %v, want [scope-orphaned]", result.SupersededScopeIDs)
		}

		// Verify gen-orphaned-old is now superseded in DB.
		var orphanedOldStatus string
		if err := db.QueryRowContext(ctx,
			"SELECT status FROM scope_generations WHERE generation_id = 'gen-orphaned-old'",
		).Scan(&orphanedOldStatus); err != nil {
			t.Fatalf("query gen-orphaned-old status: %v", err)
		}
		if orphanedOldStatus != "superseded" {
			t.Fatalf("gen-orphaned-old status = %q, want 'superseded'", orphanedOldStatus)
		}

		// Verify gen-orphaned-new is still active.
		var orphanedNewStatus string
		if err := db.QueryRowContext(ctx,
			"SELECT status FROM scope_generations WHERE generation_id = 'gen-orphaned-new'",
		).Scan(&orphanedNewStatus); err != nil {
			t.Fatalf("query gen-orphaned-new status: %v", err)
		}
		if orphanedNewStatus != "active" {
			t.Fatalf("gen-orphaned-new status = %q, want 'active'", orphanedNewStatus)
		}

		// Second sweep is idempotent: no more orphaned actives for scope-orphaned.
		result2, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("second RecoverWedgedGenerations() error = %v", err)
		}
		if result2.Superseded != 0 {
			t.Fatalf("second sweep Superseded = %d, want 0 (idempotent)", result2.Superseded)
		}
	})

	// Refresh scope-orphaned-old status from DB for scenario assertions below.
	// After the supersede pass above, scope-orphaned now has one active: gen-orphaned-new.
	// We need gen-orphaned-new to be the active_generation_id for scope-orphaned so the
	// wedged-recovery gate works (scope.active_generation_id = generation.generation_id).
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = 'gen-orphaned-new' WHERE scope_id = 'scope-orphaned'",
	); err != nil {
		t.Fatalf("update scope-orphaned active_generation_id: %v", err)
	}

	// ---------------------------------------------------------------------------
	// Scenario 1: wedged re-drive.
	// gen-wedged: active for scope-wedged, activated 2h ago, has an outstanding
	// shared_projection_intents row (completed_at IS NULL), no newer generation
	// for the scope. First sweep must re-enqueue exactly one projector work item
	// with liveness_recovery_attempts = 1.
	// ---------------------------------------------------------------------------
	t.Run("WedgedReDrive", func(t *testing.T) {
		result, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("RecoverWedgedGenerations() error = %v", err)
		}
		if got, want := result.Recovered, 1; got != want {
			t.Fatalf("Recovered = %d, want %d", got, want)
		}
		if len(result.RecoveredScopeIDs) != 1 || result.RecoveredScopeIDs[0] != "scope-wedged" {
			t.Fatalf("RecoveredScopeIDs = %v, want [scope-wedged]", result.RecoveredScopeIDs)
		}

		// Verify the projector work item exists and has liveness_recovery_attempts = 1.
		var attempts int
		if err := db.QueryRowContext(ctx, `
			SELECT (payload ->> 'liveness_recovery_attempts')::int
			FROM fact_work_items
			WHERE scope_id = 'scope-wedged'
			  AND generation_id = 'gen-wedged'
			  AND stage = 'projector'
		`).Scan(&attempts); err != nil {
			t.Fatalf("query liveness_recovery_attempts: %v", err)
		}
		if attempts != 1 {
			t.Fatalf("liveness_recovery_attempts = %d, want 1", attempts)
		}

		// Second sweep: budget ceiling is MaxRecoverAttempts = 1, so the work item
		// already has liveness_recovery_attempts = 1 which equals the ceiling. The
		// wedged CTE must exclude it and return zero recovered.
		result2, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("second RecoverWedgedGenerations() error = %v", err)
		}
		if result2.Recovered != 0 {
			t.Fatalf("second sweep Recovered = %d, want 0 (budget exhausted)", result2.Recovered)
		}

		// The work item counter must NOT exceed the ceiling.
		var attemptsAfter int
		if err := db.QueryRowContext(ctx, `
			SELECT (payload ->> 'liveness_recovery_attempts')::int
			FROM fact_work_items
			WHERE scope_id = 'scope-wedged'
			  AND generation_id = 'gen-wedged'
			  AND stage = 'projector'
		`).Scan(&attemptsAfter); err != nil {
			t.Fatalf("query liveness_recovery_attempts after second sweep: %v", err)
		}
		if attemptsAfter != 1 {
			t.Fatalf("liveness_recovery_attempts after second sweep = %d, want 1 (LEAST cap)", attemptsAfter)
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario 3: no-op safety.
	// scope-fresh: active but activated_at is recent → must not be re-driven.
	// scope-pending-newer: active but has a newer 'pending' generation → must not
	// be re-driven (projector supersede path owns it).
	// ---------------------------------------------------------------------------
	t.Run("NoOpSafety", func(t *testing.T) {
		// Run a fresh sweep. scope-wedged budget is exhausted (scenario 1) and
		// scope-orphaned is clean (scenario 2). Only scope-fresh and
		// scope-pending-newer could be candidates — verify neither is touched.
		result, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("RecoverWedgedGenerations() error = %v", err)
		}
		if result.Recovered != 0 {
			t.Fatalf("no-op sweep Recovered = %d, want 0 (fresh and pending-newer scopes must be left alone)", result.Recovered)
		}
		if result.Superseded != 0 {
			t.Fatalf("no-op sweep Superseded = %d, want 0", result.Superseded)
		}

		// Confirm scope-fresh generation status unchanged.
		var freshStatus string
		if err := db.QueryRowContext(ctx,
			"SELECT status FROM scope_generations WHERE generation_id = 'gen-fresh'",
		).Scan(&freshStatus); err != nil {
			t.Fatalf("query gen-fresh status: %v", err)
		}
		if freshStatus != "active" {
			t.Fatalf("gen-fresh status = %q, want 'active'", freshStatus)
		}

		// Confirm scope-pending-newer active generation unchanged.
		var pendingStatus string
		if err := db.QueryRowContext(ctx,
			"SELECT status FROM scope_generations WHERE generation_id = 'gen-pending-active'",
		).Scan(&pendingStatus); err != nil {
			t.Fatalf("query gen-pending-active status: %v", err)
		}
		if pendingStatus != "active" {
			t.Fatalf("gen-pending-active status = %q, want 'active'", pendingStatus)
		}

		// Confirm no projector work item created for scope-fresh or scope-pending-newer.
		var noOpCount int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM fact_work_items
			WHERE scope_id IN ('scope-fresh', 'scope-pending-newer')
			  AND stage = 'projector'
		`).Scan(&noOpCount); err != nil {
			t.Fatalf("count no-op work items: %v", err)
		}
		if noOpCount != 0 {
			t.Fatalf("projector work items for safe scopes = %d, want 0", noOpCount)
		}
	})
}

// generationLivenessProofSchemaSQL creates the minimal table set needed by the
// liveness SQL queries. The full production schema lives in schema.go and
// shared_intents.go; this minimal version covers exactly the columns referenced
// by the two liveness queries and the CountActiveGenerationsByAge query.
const generationLivenessProofSchemaSQL = `
CREATE TABLE ingestion_scopes (
    scope_id            TEXT PRIMARY KEY,
    scope_kind          TEXT NOT NULL,
    source_system       TEXT NOT NULL,
    source_key          TEXT NOT NULL,
    parent_scope_id     TEXT NULL,
    collector_kind      TEXT NOT NULL,
    partition_key       TEXT NOT NULL,
    observed_at         TIMESTAMPTZ NOT NULL,
    ingested_at         TIMESTAMPTZ NOT NULL,
    status              TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE scope_generations (
    generation_id   TEXT PRIMARY KEY,
    scope_id        TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind    TEXT NOT NULL,
    freshness_hint  TEXT NULL,
    source_commit_sha TEXT NULL,
    is_delta        BOOLEAN NOT NULL DEFAULT false,
    observed_at     TIMESTAMPTZ NOT NULL,
    ingested_at     TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL,
    activated_at    TIMESTAMPTZ NULL,
    superseded_at   TIMESTAMPTZ NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE fact_work_items (
    work_item_id    TEXT PRIMARY KEY,
    scope_id        TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id   TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    stage           TEXT NOT NULL,
    domain          TEXT NOT NULL,
    conflict_domain TEXT NOT NULL DEFAULT 'scope',
    conflict_key    TEXT NULL,
    status          TEXT NOT NULL,
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    lease_owner     TEXT NULL,
    claim_until     TIMESTAMPTZ NULL,
    visible_at      TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class   TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

CREATE TABLE shared_projection_intents (
    intent_id         TEXT PRIMARY KEY,
    projection_domain TEXT NOT NULL,
    partition_key     TEXT NOT NULL,
    scope_id          TEXT NOT NULL DEFAULT '',
    acceptance_unit_id TEXT NOT NULL DEFAULT '',
    repository_id     TEXT NOT NULL,
    source_run_id     TEXT NOT NULL,
    generation_id     TEXT NOT NULL,
    partition_hash    NUMERIC(20, 0) NULL,
    payload           JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    completed_at      TIMESTAMPTZ NULL
);
`

// generationLivenessProofSeedSQL inserts the minimal fixture set for the four
// scenarios. All timestamps are relative to now() so the test is wall-clock safe.
//
// Scopes and generations:
//
//   - scope-wedged / gen-wedged: active, activated 2 hours ago, has an
//     outstanding shared_projection_intents row → wedged (stuck + eligible for
//     recovery).
//
//   - scope-fresh / gen-fresh: active, activated 5 minutes ago → fresh,
//     must not be re-driven.
//
//   - scope-aging / gen-aging: active, activated 20 minutes ago, no outstanding
//     intents → aging, must not be re-driven (healthy quiet scope).
//
//   - scope-orphaned / gen-orphaned-old + gen-orphaned-new: two active
//     generations for the same scope. gen-orphaned-new has a later ingested_at
//     so it is the authoritative one; gen-orphaned-old must be superseded.
//
//   - scope-pending-newer / gen-pending-active + gen-pending-newer: gen-pending-active
//     is active and wedged-looking, but gen-pending-newer exists as 'pending' →
//     the NOT EXISTS gate must exclude scope-pending-newer from re-drive.
const generationLivenessProofSeedSQL = `
-- scope-wedged: wedged active generation with outstanding downstream work.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-wedged', 'repository', 'github', 'acme/wedged', 'git',
    'acme/wedged', now(), now(), 'active', 'gen-wedged'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-wedged', 'scope-wedged', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
-- Outstanding intent: completed_at IS NULL → downstream is blocked.
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-wedged', 'graph', 'acme/wedged', 'scope-wedged',
    '', 'acme/wedged', 'run-wedged', 'gen-wedged',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);

-- scope-fresh: recently activated → must not be re-driven.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-fresh', 'repository', 'github', 'acme/fresh', 'git',
    'acme/fresh', now(), now(), 'active', 'gen-fresh'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-fresh', 'scope-fresh', 'push',
    now() - interval '5 minutes', now() - interval '5 minutes',
    'active', now() - interval '5 minutes'
);

-- scope-aging: activated 20 minutes ago (past half the 30-min deadline → aging),
-- no outstanding intents → healthy quiet scope, counting as aging not stuck.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-aging', 'repository', 'github', 'acme/aging', 'git',
    'acme/aging', now(), now(), 'active', 'gen-aging'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-aging', 'scope-aging', 'push',
    now() - interval '20 minutes', now() - interval '20 minutes',
    'active', now() - interval '20 minutes'
);
-- Completed intent: completed_at IS NOT NULL → downstream is drained, not stuck.
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at, completed_at
) VALUES (
    'intent-aging', 'graph', 'acme/aging', 'scope-aging',
    '', 'acme/aging', 'run-aging', 'gen-aging',
    '{"action":"sync"}'::jsonb, now() - interval '20 minutes', now() - interval '10 minutes'
);

-- scope-orphaned: two active generations; gen-orphaned-new has a later ingested_at.
-- The unique-active index is omitted in the proof schema so two active rows coexist.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-orphaned', 'repository', 'github', 'acme/orphaned', 'git',
    'acme/orphaned', now(), now(), 'active', 'gen-orphaned-old'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES
    (
        'gen-orphaned-old', 'scope-orphaned', 'push',
        now() - interval '3 hours', now() - interval '3 hours',
        'active', now() - interval '3 hours'
    ),
    (
        'gen-orphaned-new', 'scope-orphaned', 'push',
        now() - interval '1 hour', now() - interval '1 hour',
        'active', now() - interval '1 hour'
    );

-- scope-pending-newer: active generation with a newer pending generation →
-- must be excluded from re-drive by the NOT EXISTS gate.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-pending-newer', 'repository', 'github', 'acme/pending-newer', 'git',
    'acme/pending-newer', now(), now(), 'active', 'gen-pending-active'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES
    (
        'gen-pending-active', 'scope-pending-newer', 'push',
        now() - interval '2 hours', now() - interval '2 hours',
        'active', now() - interval '2 hours'
    ),
    (
        'gen-pending-newer', 'scope-pending-newer', 'push',
        now() - interval '30 minutes', now() - interval '30 minutes',
        'pending', NULL
    );
-- Outstanding intent to make gen-pending-active look wedged, but the NOT EXISTS
-- on pending/active newer sibling must exclude it before the wedge check fires.
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-pending-newer', 'graph', 'acme/pending-newer', 'scope-pending-newer',
    '', 'acme/pending-newer', 'run-pending-newer', 'gen-pending-active',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
`
