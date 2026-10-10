// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestGenerationRetentionLockSetMatchesEligibleScopesLive is P2: the scope
// lock set a second session observes equals exactly the scopes that own an
// eligible generation; a scope whose only old generation has live work is
// not locked. Five scopes each own one eligible generation; one more scope's
// only old generation would be eligible by rank and age alone but has a
// running fact_work_items row.
func TestGenerationRetentionLockSetMatchesEligibleScopesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	old := now.Add(-10 * 24 * time.Hour)

	var eligibleScopes []string
	for i := 0; i < 5; i++ {
		scopeID := retentionSelectionScopeID("lockset", i)
		testfixtures.SeedScope(t, ctx, database, scopeID)
		testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, scopeID+"-g0", old)
		eligibleScopes = append(eligibleScopes, scopeID)
	}
	liveWorkScope := "lockset-live"
	testfixtures.SeedScope(t, ctx, database, liveWorkScope)
	liveWorkGeneration := liveWorkScope + "-g0"
	testfixtures.SeedSupersededGeneration(t, ctx, database, liveWorkScope, liveWorkGeneration, old)
	seedRetentionSelectionLiveWork(t, ctx, database, liveWorkScope, liveWorkGeneration)

	allScopes := append(append([]string{}, eligibleScopes...), liveWorkScope)

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, generationRetentionCandidateQuery, now.Add(-7*24*time.Hour), 0, 100, now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("candidate query: %v", err)
	}
	candidateScopes := map[string]bool{}
	for rows.Next() {
		var scopeID, generationID, scopeKind string
		var supersededAt, observedAt time.Time
		var uncovered bool
		if err := rows.Scan(&scopeID, &generationID, &scopeKind, &supersededAt, &observedAt, &uncovered); err != nil {
			_ = rows.Close()
			t.Fatalf("scan candidate: %v", err)
		}
		candidateScopes[scopeID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("candidate query: %v", err)
	}
	_ = rows.Close()

	// tx stays open (uncommitted, unrolled-back until the deferred Rollback)
	// so its FOR UPDATE OF scope SKIP LOCKED locks are still held while a
	// second session probes them.
	locked := probeLockedScopeSet(t, ctx, database, allScopes)

	for _, scopeID := range eligibleScopes {
		if !locked[scopeID] {
			t.Errorf("eligible scope %s not locked, want locked", scopeID)
		}
		if !candidateScopes[scopeID] {
			t.Errorf("eligible scope %s produced no candidate row", scopeID)
		}
	}
	if locked[liveWorkScope] {
		t.Errorf("scope %s (only generation has live work) is locked, want not locked", liveWorkScope)
	}
	if candidateScopes[liveWorkScope] {
		t.Errorf("scope %s (only generation has live work) produced a candidate row", liveWorkScope)
	}
	if got, want := len(locked), len(eligibleScopes); got != want {
		t.Fatalf("locked scope count = %d, want %d: %v", got, want, locked)
	}
}

// probeLockedScopeSet opens a second transaction and attempts FOR UPDATE SKIP
// LOCKED on every id in scopeIDs; the ones it cannot lock (skipped, so absent
// from its result) are the ones another session already holds.
func probeLockedScopeSet(t *testing.T, ctx context.Context, database *sql.DB, scopeIDs []string) map[string]bool {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("probe begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx,
		`SELECT scope_id FROM ingestion_scopes WHERE scope_id = ANY($1::text[]) FOR UPDATE SKIP LOCKED`, scopeIDs)
	if err != nil {
		t.Fatalf("probe lock: %v", err)
	}
	defer func() { _ = rows.Close() }()
	unlocked := map[string]bool{}
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			t.Fatalf("probe scan: %v", err)
		}
		unlocked[scopeID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("probe lock: %v", err)
	}
	locked := map[string]bool{}
	for _, scopeID := range scopeIDs {
		if !unlocked[scopeID] {
			locked[scopeID] = true
		}
	}
	return locked
}

// TestGenerationRetentionFirstCountRunsWithoutScopeLocksLive is #7334 fix
// 1: the pass's first (full-batch) row count must run after the selection's
// scope locks are released, so a production-shaped 26s count never holds the
// scope rows a concurrent fact insert waits on. A probe database intercepts
// the first generation_retention_row_counts statement and asks a second
// session which candidate scopes are locked at that moment. RED: both
// candidate scopes are still locked while the count runs.
//
// The generations stay countable (50 own facts under a limit of 100): an
// over-fact generation never reaches the full count since #7334 fix 2's
// pre-screen, so it cannot prove anything about the count's lock hold.
func TestGenerationRetentionFirstCountRunsWithoutScopeLocksLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	old := now.Add(-10 * 24 * time.Hour)

	// Two scopes, one eligible generation each, 50 own facts against a row
	// limit of 100: the pass selects both, counts both unlocked, re-locks
	// both and prunes both.
	var scopes []string
	for i := 0; i < 2; i++ {
		scopeID := retentionSelectionScopeID("nolock", i)
		testfixtures.SeedScope(t, ctx, database, scopeID)
		generationID := scopeID + "-g0"
		testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, generationID, old)
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'nolock/' || $1 || '/' || i, $1, $2, 'repository', 'k' || i, 'git', 'k' || i, now(), now(), '{}'::jsonb
FROM generate_series(1, 50) AS i`,
			scopeID, generationID,
		); err != nil {
			t.Fatalf("seed facts: %v", err)
		}
		scopes = append(scopes, scopeID)
	}
	policy := GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "7334-count-unlocked",
	}

	probe := &retentionCountLockProbeDB{inner: SQLDB{DB: database}}
	probe.onFirstCount = func() {
		probe.lockedDuringCount = probeLockedScopeSet(t, ctx, database, scopes)
	}
	store := NewGenerationRetentionStore(probe)
	result, err := store.PruneSupersededGenerations(ctx, policy)
	if err != nil {
		t.Fatalf("first prune: %v", err)
	}
	if probe.countCalls == 0 {
		t.Fatal("count query never ran; the lock probe observed nothing")
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2 (both candidates within the limit)", result.GenerationsPruned)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want no skips", result.Skipped)
	}
	for scopeID := range probe.lockedDuringCount {
		t.Errorf("scope %s locked while the first row count ran, want the count unlocked", scopeID)
	}
}

// retentionCountLockProbeDB wraps the store's database to run onFirstCount
// synchronously when the retention transaction issues its first row-count
// statement, while that statement is still waiting to execute.
type retentionCountLockProbeDB struct {
	inner             SQLDB
	onFirstCount      func()
	countCalls        int
	lockedDuringCount map[string]bool
}

func (p *retentionCountLockProbeDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.inner.ExecContext(ctx, query, args...)
}

func (p *retentionCountLockProbeDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return p.inner.QueryContext(ctx, query, args...)
}

func (p *retentionCountLockProbeDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &retentionCountLockProbeTx{Transaction: tx, probe: p}, nil
}

type retentionCountLockProbeTx struct {
	db.Transaction
	probe *retentionCountLockProbeDB
}

func (tx *retentionCountLockProbeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "generation_retention_row_counts") {
		tx.probe.countCalls++
		if tx.probe.countCalls == 1 && tx.probe.onFirstCount != nil {
			tx.probe.onFirstCount()
		}
	}
	return tx.Transaction.QueryContext(ctx, query, args...)
}
