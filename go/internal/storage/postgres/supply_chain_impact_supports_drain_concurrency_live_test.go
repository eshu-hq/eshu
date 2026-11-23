// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11198 hot-key drain concurrency on real PostgreSQL: while a supports drain runs page by page, writers insert rows below and above its cursor, retract and rewrite rows, supersede a generation and delete a generation by cascade; every row present and active for the whole drain is returned exactly once, no row that was absent or inactive for the whole drain is returned, a row locked by an open writer transaction does not make a page wait, and no transaction stays open between pages
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

const concKey = "pkg:conc"

// concInsert writes count supports rows for the key straight into the table, ids
// prefix + a five-digit sequence, in the given generation.
func concInsert(t *testing.T, ctx context.Context, db *sql.DB, prefix string, count int, scope, generation string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
  priority_score, source_confidence, suppression_state, package_id, payload)
SELECT $1 || lpad(i::text, 5, '0'), 'k-' || $1 || i, $2, $3, 'f-' || $1 || i, 1, 50, 'exact', 'none', $4,
       jsonb_build_object('group', $1::text, 'package_id', $4::text)
FROM generate_series(1, $5::int) AS i`, prefix, scope, generation, concKey, count); err != nil {
		t.Fatalf("seed %s: %v", prefix, err)
	}
}

// TestSupplyChainImpactSupportsDrainConcurrentWithWritesLive is proof 5 of the
// hot-key ruling.
func TestSupplyChainImpactSupportsDrainConcurrentWithWritesLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "b2c_drain_concurrency",
		workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
	b2cSeedScopes(t, ctx, db)
	now := time.Now()
	for _, stmt := range []string{
		// A scope whose superseded generation is deleted mid-drain (cascade).
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
		 VALUES ('scope:conc:d', 'repository', 'git', 'scope:conc:d', 'git', 'scope:conc:d', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
		 VALUES ('gen:conc:d-old', 'scope:conc:d', 'synthetic', now(), now(), 'superseded', now(), '{}'::jsonb),
		        ('gen:conc:d', 'scope:conc:d', 'synthetic', now(), now(), 'active', now(), '{}'::jsonb),
		        ('gen:conc:b2', 'scope:11198:b', 'synthetic', now(), now(), 'pending', now(), '{}'::jsonb)`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen:conc:d' WHERE scope_id = 'scope:conc:d'`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed scopes: %v (%s)", err, strings.Fields(stmt)[0])
		}
	}
	_ = now
	// Present and active for the whole drain: 2,000 rows (four pages) in scope A.
	concInsert(t, ctx, db, "s-", 2000, b2cScopeA, b2cGenA)
	// Inactive for the whole drain: a superseded generation, interleaved by id.
	concInsert(t, ctx, db, "s-i-", 600, b2cScopeB, b2cGenBOld)
	concInsert(t, ctx, db, "s-x-", 300, "scope:conc:d", "gen:conc:d-old")
	// Rows the writers change mid-drain (either outcome is allowed for them).
	concInsert(t, ctx, db, "m-", 600, b2cScopeA, b2cGenA)
	// Scope B's active generation: superseded mid-drain.
	concInsert(t, ctx, db, "sb-", 300, b2cScopeB, b2cGenB)

	drain := newSupplyChainImpactSupportsDrain(nil, "drain_concurrency", securityAlertReconciliationSupportsArms,
		[]string(nil), []string{concKey}, []string(nil), []string(nil))

	// A writer transaction holds a row lock on a stable row for the whole drain.
	locker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin locker: %v", err)
	}
	defer func() { _ = locker.Rollback() }()
	if _, err := locker.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports SET priority_score = 51 WHERE fact_id = 's-00010'`); err != nil {
		t.Fatalf("lock a row: %v", err)
	}

	idleInTransaction := func() int {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state = 'idle in transaction' AND pid <> pg_backend_pid() AND query NOT LIKE 'UPDATE supply_chain_impact_finding_supports SET priority_score%'`).Scan(&n); err != nil {
			t.Fatalf("probe idle transactions: %v", err)
		}
		return n
	}
	mutate := func(stmts ...string) {
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Errorf("mutation %q: %v", strings.Fields(stmt)[0]+"...", err)
			}
		}
	}
	idleSeen := 0
	drain.afterPage = func(page int) {
		idleSeen += idleInTransaction()
		switch page {
		case 1:
			// Rows below and above the cursor appear; half of the mutable rows are
			// retracted and half rewritten; scope B's generation is superseded.
			concInsert(t, ctx, db, "0-new-", 50, b2cScopeA, b2cGenA)
			concInsert(t, ctx, db, "z-new-", 50, b2cScopeA, b2cGenA)
			mutate(
				`DELETE FROM supply_chain_impact_finding_supports WHERE fact_id LIKE 'm-%' AND right(fact_id, 1) IN ('0','1','2','3','4')`,
				`UPDATE supply_chain_impact_finding_supports SET payload = payload || '{"rewritten":true}'::jsonb WHERE fact_id LIKE 'm-%'`,
				`UPDATE scope_generations SET status = 'superseded' WHERE generation_id = 'generation:11198:b'`,
				`UPDATE scope_generations SET status = 'active' WHERE generation_id = 'gen:conc:b2'`,
				`UPDATE ingestion_scopes SET active_generation_id = 'gen:conc:b2' WHERE scope_id = 'scope:11198:b'`,
			)
		case 2:
			// A generation delete cascades its supports away.
			mutate(`DELETE FROM scope_generations WHERE generation_id = 'gen:conc:d-old'`)
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	got, err := drain.run(runCtx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("drain error = %v, want nil (a row lock or a delete must not fail or stall a page)", err)
	}
	_ = locker.Rollback()

	returned := map[string]int{}
	for _, envelope := range got {
		returned[envelope.FactID]++
	}
	for id, n := range returned {
		if n != 1 {
			t.Errorf("row %s returned %d times, want exactly once", id, n)
		}
		if strings.HasPrefix(id, "s-i-") || strings.HasPrefix(id, "s-x-") {
			t.Errorf("row %s returned: it was inactive for the whole drain", id)
		}
	}
	for i := 1; i <= 2000; i++ {
		id := "s-" + leftPad5(i)
		if returned[id] != 1 {
			t.Errorf("stable row %s returned %d times, want exactly once", id, returned[id])
		}
	}
	if idleSeen != 0 {
		t.Errorf("a transaction was idle in transaction between pages %d times, want 0", idleSeen)
	}
	if len(got) < 2000 {
		t.Fatalf("drain returned %d rows, want at least the 2,000 stable rows", len(got))
	}
}

// leftPad5 renders i as the five-digit sequence the seed ids use.
func leftPad5(i int) string { return fmt.Sprintf("%05d", i) }
