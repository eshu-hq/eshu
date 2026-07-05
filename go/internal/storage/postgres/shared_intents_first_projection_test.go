// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestScopeHasPriorGenerationSQLShape locks the first-projection probe query
// shape (#3624): it keys on scope_id and excludes the current generation, and it
// must NOT filter on activated_at — these domains write edges on acceptance
// (before activation), so a superseded-while-pending generation can have written
// edges without ever activating, and keying on activation would let the skip
// leave those edges stale.
func TestScopeHasPriorGenerationSQLShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM scope_generations",
		"WHERE scope_id = $1",
		"generation_id <> $2",
		"SELECT EXISTS (",
	} {
		if !strings.Contains(scopeHasPriorGenerationSQL, want) {
			t.Fatalf("scopeHasPriorGenerationSQL missing %q:\n%s", want, scopeHasPriorGenerationSQL)
		}
	}
	if strings.Contains(scopeHasPriorGenerationSQL, "activated_at") {
		t.Fatalf("scopeHasPriorGenerationSQL must not key on activated_at (edges write pre-activation):\n%s", scopeHasPriorGenerationSQL)
	}
}

// TestSharedIntentStoreSatisfiesFirstProjectionLookup pins the compile-time
// contract that the runtime store answers the reducer's first-projection probe.
func TestSharedIntentStoreSatisfiesFirstProjectionLookup(t *testing.T) {
	t.Parallel()

	var _ reducer.FirstProjectionLookup = (*SharedIntentStore)(nil)
}

// TestScopeHasPriorGenerationAgainstPostgres proves the probe behavior against a
// real Postgres: false when the scope's only generation is the current one
// (first projection → skip), and true once ANY other generation exists —
// including a superseded-while-pending generation that never activated, the
// exact case a naive activated_at filter would miss. Set
// ESHU_FIRST_PROJECTION_PROOF_DSN to run it; skipped otherwise.
func TestScopeHasPriorGenerationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ESHU_FIRST_PROJECTION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_FIRST_PROJECTION_PROOF_DSN to run the first-projection probe proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	scopeID := "first-projection-proof:" + time.Now().UTC().Format("20060102150405.000000000")
	curGen := scopeID + ":gen-current"
	priorGen := scopeID + ":gen-prior-pending"
	now := time.Now().UTC()

	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
		VALUES ($1,'git','git-collector',$1,'git',$1,$2,$2,'active','{}'::jsonb)`, scopeID, now); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM ingestion_scopes WHERE scope_id=$1`, scopeID)
	})

	insertGen := func(genID, status string, activatedAt *time.Time) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
			VALUES ($1,$2,'sync',$3,$3,$4,$5,'{}'::jsonb)`, genID, scopeID, now, status, activatedAt); err != nil {
			t.Fatalf("insert generation %s: %v", genID, err)
		}
	}

	store := NewSharedIntentStore(SQLDB{DB: db})

	// Only the current generation exists → first projection → false (skip).
	insertGen(curGen, "active", &now)
	got, err := store.ScopeHasPriorGeneration(ctx, scopeID, curGen)
	if err != nil {
		t.Fatalf("probe (current only): %v", err)
	}
	if got {
		t.Fatalf("ScopeHasPriorGeneration(current only) = true, want false")
	}

	// A prior generation that was superseded while still pending (never
	// activated: activated_at NULL) → true (do not skip), because its edges may
	// already be in the graph.
	insertGen(priorGen, "superseded", nil)
	got, err = store.ScopeHasPriorGeneration(ctx, scopeID, curGen)
	if err != nil {
		t.Fatalf("probe (with prior pending): %v", err)
	}
	if !got {
		t.Fatalf("ScopeHasPriorGeneration(with superseded-never-activated prior) = false, want true")
	}
}
