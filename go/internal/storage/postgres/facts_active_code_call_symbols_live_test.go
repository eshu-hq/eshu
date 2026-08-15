// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

const activeCodeCallSymbolProofKey = "scip-go gomod github.com/acme/lib Client#Request()."

// TestReducerContentionGateActiveCodeCallSymbolLoaderCrossRepository proves
// the production loader resolves an active definition from a repository other
// than the caller. Its prefix enrolls it in the blocking real-Postgres reducer
// contention job; the hermetic workflow guard pins that enrollment.
func TestReducerContentionGateActiveCodeCallSymbolLoaderCrossRepository(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the real-Postgres loader proof")
	}

	ctx := context.Background()
	db, _ := openFactCrossBatchFencingSchema(t, ctx, dsn)
	now := time.Now().UTC()
	seedActiveCodeCallSymbolScope(t, ctx, db, "repository:repo-api", "generation-api", now)
	seedActiveCodeCallSymbolScope(t, ctx, db, "repository:repo-lib", "generation-lib", now)
	seedActiveCodeCallSymbolFact(t, ctx, db, "fact-api-caller", "repository:repo-api", "generation-api", "api.go", "scip-go gomod github.com/acme/api Handler#Serve().", now)
	seedActiveCodeCallSymbolFact(t, ctx, db, "fact-lib-active", "repository:repo-lib", "generation-lib", "client.go", activeCodeCallSymbolProofKey, now.Add(time.Second))
	seedActiveCodeCallSymbolFact(t, ctx, db, "fact-lib-stale", "repository:repo-lib", "generation-lib-stale", "old_client.go", activeCodeCallSymbolProofKey, now.Add(-time.Second))

	loaded, err := NewFactStore(SQLDB{DB: db}).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{activeCodeCallSymbolProofKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() len = %d, want %d active cross-repository definition", got, want)
	}
	if got, want := loaded[0].FactID, "fact-lib-active"; got != want {
		t.Fatalf("loaded FactID = %q, want %q", got, want)
	}
}

func seedActiveCodeCallSymbolScope(t *testing.T, ctx context.Context, db Executor, scopeID, generationID string, observedAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ($1, 'repository', 'git', $1, 'git', $1, $3, $3, 'active', $2)`, scopeID, generationID, observedAt); err != nil {
		t.Fatalf("insert scope %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES ($1, $2, 'snapshot', $3, $3, 'active', $3)`, generationID, scopeID, observedAt); err != nil {
		t.Fatalf("insert generation %q: %v", generationID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ($1, $2, 'snapshot', $3, $3, 'superseded')`, generationID+"-stale", scopeID, observedAt.Add(-time.Minute)); err != nil {
		t.Fatalf("insert stale generation for %q: %v", scopeID, err)
	}
}

func seedActiveCodeCallSymbolFact(t *testing.T, ctx context.Context, db Executor, factID, scopeID, generationID, relativePath, symbol string, observedAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES (
    $1, $2, $3, 'file', 'file:' || $2 || ':' || $4,
    'git', $4, $6, $6,
    jsonb_build_object(
        'repo_id', $2,
        'relative_path', $4,
        'parsed_file_data', jsonb_build_object(
            'functions', jsonb_build_array(jsonb_build_object('uid', 'uid:' || $1, 'scip_symbol', $5::text))
        )
    )
)`, factID, scopeID, generationID, relativePath, symbol, observedAt); err != nil {
		t.Fatalf("insert fact %q: %v", factID, err)
	}
}
