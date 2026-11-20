// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// liveFixture is one disposable, fully migrated database. Generation ids carry
// the test's own prefix because advisory locks are database-wide.
type liveFixture struct {
	ctx      context.Context
	db       *sql.DB
	database postgres.SQLDB
}

// openLiveFixture opens a disposable PostgreSQL database with the real
// bootstrap schema applied, or skips when ESHU_POSTGRES_DSN is not set.
func openLiveFixture(t *testing.T) *liveFixture {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN (and ESHU_POSTGRES_DISPOSABLE=1) to run the relationship retention live proofs")
	}
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, os.Getenv("ESHU_POSTGRES_DISPOSABLE"),
		5*time.Minute, func(ctx context.Context, db *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db})
		})
	database.SetMaxOpenConns(16)
	return &liveFixture{ctx: ctx, db: database, database: postgres.SQLDB{DB: database}}
}

func (f *liveFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.ExecContext(f.ctx, query, args...); err != nil {
		t.Fatalf("exec %.80q: %v", query, err)
	}
}

// scope inserts an ingestion scope with no active generation.
func (f *liveFixture) scope(t *testing.T, scopeID string) {
	t.Helper()
	f.exec(t, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', '{}'::jsonb)`, scopeID)
}

// scopeGeneration inserts a scope_generations row in the given status.
func (f *liveFixture) scopeGeneration(t *testing.T, scopeID, generationID, status string) {
	t.Helper()
	f.exec(t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', now(), now(), $3, '{}'::jsonb)`, generationID, scopeID, status)
}

// pointer sets a scope's active generation pointer.
func (f *liveFixture) pointer(t *testing.T, scopeID, generationID string) {
	t.Helper()
	f.exec(t, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID)
}

// generation inserts a relationship_generations row.
func (f *liveFixture) generation(t *testing.T, scopeID, generationID, status string) {
	t.Helper()
	activated := "NULL"
	if status == "active" {
		activated = "now()"
	}
	f.exec(t, fmt.Sprintf(`INSERT INTO relationship_generations (generation_id, scope, run_id, status, created_at, activated_at)
VALUES ($1, $2, 'run-1', $3, '2026-01-01T00:00:00Z', %s)`, activated), generationID, scopeID, status)
}

// evidence inserts n evidence rows for the generation.
func (f *liveFixture) evidence(t *testing.T, generationID string, n int) {
	t.Helper()
	f.exec(t, `INSERT INTO relationship_evidence_facts (evidence_id, generation_id, evidence_kind, relationship_type,
    source_repo_id, target_repo_id, confidence, rationale, details, observed_at)
SELECT $1::text || '-ev-' || i, $1, 'terraform', 'DEPENDS_ON', 'repo-' || i, 'repo-t', 0.9, 'seed', '{}'::jsonb, now()
FROM generate_series(1, $2::int) AS i`, generationID, n)
}

// candidates inserts n candidate rows for the generation.
func (f *liveFixture) candidates(t *testing.T, generationID string, n int) {
	t.Helper()
	f.exec(t, `INSERT INTO relationship_candidates (candidate_id, generation_id, source_repo_id, target_repo_id,
    relationship_type, confidence, evidence_count, rationale, details)
SELECT $1::text || '-cand-' || i, $1, 'repo-' || i, 'repo-t', 'DEPENDS_ON', 0.8, 1, 'seed', '{}'::jsonb
FROM generate_series(1, $2::int) AS i`, generationID, n)
}

// resolved inserts n resolved rows for the generation (an INSERT only: this
// slice never updates or deletes one).
func (f *liveFixture) resolved(t *testing.T, generationID string, n int) {
	t.Helper()
	f.exec(t, `INSERT INTO resolved_relationships (resolved_id, generation_id, source_repo_id, target_repo_id,
    relationship_type, confidence, evidence_count, rationale, resolution_source, details)
SELECT $1::text || '-res-' || i, $1, 'repo-' || i, 'repo-t', 'DEPENDS_ON', 0.9, 1, 'seed', 'inference', '{}'::jsonb
FROM generate_series(1, $2::int) AS i`, generationID, n)
}

// counts is one generation's row counts across the relationship tables.
type counts struct{ evidence, candidates, generations, resolved int64 }

// of reads the generation's row counts.
func (f *liveFixture) of(t *testing.T, generationID string) counts {
	t.Helper()
	var c counts
	if err := f.db.QueryRowContext(f.ctx, `SELECT
    (SELECT count(*) FROM relationship_evidence_facts WHERE generation_id = $1),
    (SELECT count(*) FROM relationship_candidates WHERE generation_id = $1),
    (SELECT count(*) FROM relationship_generations WHERE generation_id = $1),
    (SELECT count(*) FROM resolved_relationships WHERE generation_id = $1)`, generationID).
		Scan(&c.evidence, &c.candidates, &c.generations, &c.resolved); err != nil {
		t.Fatalf("count rows of %s: %v", generationID, err)
	}
	return c
}

// sweep runs one pass with deletes enabled and no row cap in the way.
func (f *liveFixture) sweep(t *testing.T) relationshipretention.Result {
	t.Helper()
	result, err := relationshipretention.Sweep(f.ctx, f.database, relationshipretention.Options{Deletes: true, RowCap: 1 << 40})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return result
}

// census runs the census only.
func (f *liveFixture) census(t *testing.T) relationshipretention.Result {
	t.Helper()
	result, err := relationshipretention.Sweep(f.ctx, f.database, relationshipretention.Options{})
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	return result
}

// holdGeneration parks a transaction that holds the generation row FOR UPDATE,
// the lock an activation or a refinalize holds while it writes. It returns the
// release function (rollback), also registered as cleanup.
func (f *liveFixture) holdGeneration(t *testing.T, generationID string) (release func()) {
	t.Helper()
	return f.holdRows(t, `SELECT 1 FROM relationship_generations WHERE generation_id = $1 FOR UPDATE`, generationID)
}

// holdEvidence parks a transaction that holds every evidence row of the
// generation FOR UPDATE, the way a writer in the middle of an upsert would. It
// returns the release function (rollback), also registered as cleanup.
func (f *liveFixture) holdEvidence(t *testing.T, generationID string) (release func()) {
	t.Helper()
	return f.holdRows(t, `SELECT 1 FROM relationship_evidence_facts WHERE generation_id = $1 FOR UPDATE`, generationID)
}

// holdCandidates parks a transaction that holds every candidate row of the
// generation FOR UPDATE, the way a writer in the middle of an upsert would. It
// returns the release function (rollback), also registered as cleanup.
func (f *liveFixture) holdCandidates(t *testing.T, generationID string) (release func()) {
	t.Helper()
	return f.holdRows(t, `SELECT 1 FROM relationship_candidates WHERE generation_id = $1 FOR UPDATE`, generationID)
}

// holdRows runs a locking SELECT in a transaction it leaves open and returns
// the release function (rollback), also registered as cleanup.
func (f *liveFixture) holdRows(t *testing.T, lockingSelect string, args ...any) (release func()) {
	t.Helper()
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	rows, err := tx.QueryContext(f.ctx, lockingSelect, args...)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("hold rows %.60q: %v", lockingSelect, err)
	}
	held := 0
	for rows.Next() {
		held++
	}
	if err := rows.Err(); err != nil || held == 0 {
		_ = rows.Close()
		_ = tx.Rollback()
		t.Fatalf("hold rows %.60q: locked %d rows, error %v", lockingSelect, held, err)
	}
	_ = rows.Close()
	released := false
	release = func() {
		if !released {
			released = true
			_ = tx.Rollback()
		}
	}
	t.Cleanup(release)
	return release
}

// awaitCandidatesUnlocked blocks until no session holds the generation's
// candidate rows, then releases immediately without changing them. Rollback
// returns to the client before the server has necessarily shed every lock,
// so a wait-free sweep issued right after a release can still observe
// lock_busy and flake; acquiring the rows first turns that race into a
// proof — when this returns, a following sweep cannot meet a held row. A
// timeout here means a lock is genuinely stuck, not merely slow to release.
func (f *liveFixture) awaitCandidatesUnlocked(t *testing.T, generationID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin unlock waiter: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM relationship_candidates WHERE generation_id = $1 FOR UPDATE`, generationID); err != nil {
		t.Fatalf("candidate rows of %s stayed locked: %v", generationID, err)
	}
}

// eligibilityMatrix seeds the E1 to E8 matrix of the design. Every generation
// carries 3 evidence rows and 2 candidate rows (E5 and E6 are evidence only, as
// specified), and E2 also carries 2 resolved rows, which this slice must leave.
//
//	E1  in scope_generations, generation row superseded       kept (window)
//	E2  no scope_generations row, generation row superseded   swept
//	E3  no scope_generations row, generation row active       kept (stuck resolution)
//	E4  no scope_generations row, a scope pointer names it    kept (dangling pointer)
//	E5  evidence and candidates only, no generation row       swept (no_rg)
//	E6  pending scope_generations row, evidence only          kept
//	E7  refinalize: scope_generations row, superseded, pointer kept
//	E8  eligible, generation row locked by another session    skipped, swept later
func (f *liveFixture) eligibilityMatrix(t *testing.T) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for _, name := range []string{"e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8"} {
		ids[name] = "gen-" + name
		f.scope(t, "scope-"+name)
	}
	f.scopeGeneration(t, "scope-e1", ids["e1"], "superseded")
	f.generation(t, "scope-e1", ids["e1"], "superseded")

	f.generation(t, "scope-e2", ids["e2"], "superseded")

	f.generation(t, "scope-e3", ids["e3"], "active")

	f.generation(t, "scope-e4", ids["e4"], "superseded")
	f.pointer(t, "scope-e4", ids["e4"])

	f.scopeGeneration(t, "scope-e6", ids["e6"], "pending")

	f.scopeGeneration(t, "scope-e7", ids["e7"], "superseded")
	f.generation(t, "scope-e7", ids["e7"], "superseded")
	f.pointer(t, "scope-e7", ids["e7"])

	f.generation(t, "scope-e8", ids["e8"], "superseded")

	for name, id := range ids {
		f.evidence(t, id, 3)
		if name != "e6" {
			f.candidates(t, id, 2)
		}
	}
	f.resolved(t, ids["e2"], 2)
	return ids
}
