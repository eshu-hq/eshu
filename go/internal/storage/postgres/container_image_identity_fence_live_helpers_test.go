// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Shared fixtures for the real-Postgres proofs of the #5847 fencing token.
//
// The proofs themselves live in reducer_fact_batch_insert_fence_live_test.go.
// They are here rather than in package reducer because the fact row needs the
// real fact_records table with its ingestion_scopes and scope_generations
// foreign keys, its fencing_token column, and its indexes — which ApplyBootstrap
// owns. internal/storage/postgres already imports internal/reducer, so this
// direction is the one that compiles; a live test inside package reducer would
// have to hand-roll a stand-in table that shares neither the columns the guard
// reads nor the constraints production rows are subject to.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ReducerFactBatchInsert.*Live -count=1

const containerImageIdentityLiveFactKind = "reducer_container_image_identity"

// containerImageIdentityFenceLiveDB opens the DSN-gated database and applies
// the bootstrap schema, skipping the whole test when no DSN is configured.
//
// The gate is ESHU_POSTGRES_DSN alone, the same one every other live test in
// this package uses, so a developer or CI lane that already exports a DSN runs
// this proof without knowing a bespoke per-test flag exists.
//
// Schema setup and the proof itself get SEPARATE deadlines, and the returned
// context's 90s budget starts AFTER ApplyBootstrap returns. They were one budget
// before, which let one-time DDL spend the proof's time. On a fresh schema with
// the host saturated — a 1-minute load average near 200 — a live proof in this
// family failed at ~90.0s inside ApplyBootstrap, at a different bootstrap step
// each time (webhook_refresh_triggers, then service_evidence_snapshots), while
// warm re-runs against the same database passed.
//
// Cold DDL is not itself expensive, so the split is not about reclaiming time:
// against fresh containers holding 0 tables on that same host once it was back
// to an idle load, ApplyBootstrap builds all 178 tables in ~0.85-1.0s, and the
// whole test under the old single budget passes cold in ~0.87s. The budget was
// never close to short — a first-time runner on an idle machine passes, and
// only a first-time runner on a heavily loaded one sees that red.
//
// The split is about budget COUPLING, not about naming the failing phase: that
// red was already labelled "apply bootstrap schema", by this same t.Fatalf,
// before any split existed. What one shared deadline hides is a bootstrap that
// is slow but FINITE — the old context was created BEFORE ApplyBootstrap ran, so
// setup spending 30s left the proof 60s, and a write that then timed out would
// surface inside the write with nothing in the write being slow and no bootstrap
// string to correct the reading. Starting the proof's clock after ApplyBootstrap
// returns removes that coupling; 90s remains the budget for what these tests
// actually measure, so a genuine hang in the write path still trips it.
// ApplyBootstrap gets its own, larger allowance, amortized across every test
// that calls this helper: 5 minutes is headroom, not a derived size — its
// duration under that near-200 load average was never measured.
func containerImageIdentityFenceLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres reducer fact fencing-token proofs")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), 5*time.Minute)
	err = ApplyBootstrap(schemaCtx, SQLDB{DB: sqlDB})
	cancelSchema()
	if err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return sqlDB, ctx
}

// seedContainerImageIdentityScopeGeneration creates the scope and generation a
// fact row's foreign keys require. Both are per-test unique, so concurrent runs
// of this package against one database do not collide.
func seedContainerImageIdentityScopeGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID string,
) {
	t.Helper()

	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text, '{}'::jsonb)
		ON CONFLICT (scope_id) DO NOTHING`,
		scopeID, now, generationID,
	); err != nil {
		t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
		ON CONFLICT (generation_id) DO NOTHING`,
		generationID, scopeID, now,
	); err != nil {
		t.Fatalf("seed scope_generations %s: %v", generationID, err)
	}
}

// containerImageIdentityLiveWrite builds a fenced one-decision write for the
// given outcome. exact_digest and tag_resolved are the only two outcomes that
// set CanonicalWrites=1, and they produce DIFFERENT fact ids for the same image
// reference because the identity embeds the outcome — which is why two passes
// only collide on the insert's conflict key when they agree on the outcome.
func containerImageIdentityLiveWrite(
	scopeID, generationID, suffix string,
	evidenceAsOf time.Time,
	outcome reducer.ContainerImageIdentityOutcome,
) reducer.ContainerImageIdentityWrite {
	return reducer.ContainerImageIdentityWrite{
		IntentID:     "intent-" + suffix,
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "git",
		Cause:        "live fence proof",
		EvidenceAsOf: evidenceAsOf,
		Decisions: []reducer.ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/api:prod",
				Digest:           "sha256:" + "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12",
				RepositoryID:     "oci-registry://registry.example.com/team/api",
				Outcome:          outcome,
				Reason:           "live proof decision",
				CanonicalWrites:  1,
				IdentityStrength: "tag_observation_with_digest",
			},
		},
	}
}
