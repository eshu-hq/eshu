// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

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
//   - scope-reducer-backlog / gen-reducer-backlog: active, activated 2 hours
//     ago, has an outstanding shared_projection_intents row and same-generation
//     reducer fact-work still pending → aging but not stuck, and not eligible
//     for recovery until reducer work drains.
//
//   - scope-shared-backlog / gen-shared-backlog: active, activated 2 hours
//     ago, has an outstanding shared_projection_intents row and all
//     same-generation reducer fact-work already succeeded. Its source-local
//     projector work also already succeeded, which means the canonical phase
//     publication path completed before ack. This is shared-projection backlog,
//     not a source-local wedge, and must not be re-driven.
//
//   - scope-recovery-inflight / gen-recovery-inflight: active, activated 2 hours
//     ago, has an outstanding shared_projection_intents row and a pending
//     liveness re-drive projector row. It must not be selected again before the
//     pending recovery work is processed, or the runner burns the retry budget
//     in a tight loop.
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

-- scope-reducer-backlog: outstanding shared projection intent behind legitimate
-- same-generation reducer backlog. This is progress, not a wedge.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-reducer-backlog', 'repository', 'github', 'acme/reducer-backlog', 'git',
    'acme/reducer-backlog', now(), now(), 'active', 'gen-reducer-backlog'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-reducer-backlog', 'scope-reducer-backlog', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-reducer-backlog', 'graph', 'acme/reducer-backlog', 'scope-reducer-backlog',
    '', 'acme/reducer-backlog', 'run-reducer-backlog', 'gen-reducer-backlog',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at
) VALUES (
    'reducer_gen-reducer-backlog', 'scope-reducer-backlog', 'gen-reducer-backlog',
    'reducer', 'source_local', 'pending', now() - interval '2 hours', now() - interval '2 hours'
);

-- scope-shared-backlog: source-local projection already succeeded and reducer
-- fact-work has drained, but shared projection work is still pending. This is
-- normal downstream backlog, not a source-local wedge.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-shared-backlog', 'repository', 'github', 'acme/shared-backlog', 'git',
    'acme/shared-backlog', now(), now(), 'active', 'gen-shared-backlog'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-shared-backlog', 'scope-shared-backlog', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-shared-backlog', 'repo_dependency', 'acme/shared-backlog', 'scope-shared-backlog',
    '', 'acme/shared-backlog', 'run-shared-backlog', 'gen-shared-backlog',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at
) VALUES
    (
        'projector_scope-shared-backlog_gen-shared-backlog',
        'scope-shared-backlog', 'gen-shared-backlog',
        'projector', 'source_local', 'succeeded', '{}'::jsonb,
        now() - interval '2 hours', now() - interval '2 hours'
    ),
    (
        'reducer_gen-shared-backlog',
        'scope-shared-backlog', 'gen-shared-backlog',
        'reducer', 'repo_dependency', 'succeeded', '{}'::jsonb,
        now() - interval '2 hours', now() - interval '90 minutes'
    );

-- scope-recovery-inflight: a previous liveness sweep already re-enqueued
-- source-local projector work. Do not select it again until that work exits
-- its non-terminal state.
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-recovery-inflight', 'repository', 'github', 'acme/recovery-inflight', 'git',
    'acme/recovery-inflight', now(), now(), 'active', 'gen-recovery-inflight'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-recovery-inflight', 'scope-recovery-inflight', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-recovery-inflight', 'repo_dependency', 'acme/recovery-inflight', 'scope-recovery-inflight',
    '', 'acme/recovery-inflight', 'run-recovery-inflight', 'gen-recovery-inflight',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at
) VALUES (
    'projector_scope-recovery-inflight_gen-recovery-inflight',
    'scope-recovery-inflight', 'gen-recovery-inflight',
    'projector', 'source_local', 'pending',
    '{"liveness_recovery_attempts":1}'::jsonb,
    now() - interval '1 minute', now() - interval '1 minute'
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

// generationLivenessWedgedOnlySeedSQL seeds only scope-wedged for subtest
// isolation. Use this when the test must run the wedged re-drive CTE in
// pristine state, without any prior orphaned-supersede sweep having already
// incremented liveness_recovery_attempts on gen-wedged.
const generationLivenessWedgedOnlySeedSQL = `
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
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-wedged', 'graph', 'acme/wedged', 'scope-wedged',
    '', 'acme/wedged', 'run-wedged', 'gen-wedged',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
`

// generationLivenessOrphanedOnlySeedSQL seeds only scope-orphaned for subtest
// isolation. Use this when the test must run the orphaned-supersede CTE in
// pristine state, without any wedged recovery side-effects contaminating the
// scope or fact_work_items table.
const generationLivenessOrphanedOnlySeedSQL = `
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
`

// openLivenessProofDB opens a Postgres connection for a generation liveness
// integration proof run and returns the open *sql.DB. The connection is closed
// via t.Cleanup when the test finishes. It is single-threaded (MaxOpenConns=1)
// to avoid cross-connection search_path confusion during proof runs.
func openLivenessProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// provisionLivenessSchema creates a fresh proof schema, sets the search_path on
// the connection, creates the minimal table set, and applies seedSQL when it is
// non-empty. The schema is dropped via t.Cleanup when the test finishes.
//
// Each subtest that needs full isolation must call this independently so that
// RecoverWedgedGenerations sweeps run against a pristine fixture and cannot
// observe side-effects from sibling subtests.
func provisionLivenessSchema(t *testing.T, db *sql.DB, seedSQL string) {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("liveness_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, generationLivenessProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	if seedSQL != "" {
		if _, err := db.ExecContext(ctx, seedSQL); err != nil {
			t.Fatalf("seed proof data: %v", err)
		}
	}
}
