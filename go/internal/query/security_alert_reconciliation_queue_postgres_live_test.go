// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8839 disposable PostgreSQL proof of the security_alert_reconciliation per-scope queue read: outstanding (pending, claimed, running, retrying) and dead-lettered counts per covering scope, other domains and terminal rows excluded, and EXPLAIN (ANALYZE, BUFFERS) over a 500,000-item backlog logged, not gated; disposable bootstrapped database per test
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const reconciliationQueueLiveSkip = "set ESHU_POSTGRES_DSN to run the #8839 reconciliation queue live proofs"

// reconciliationQueueLiveFixture seeds ingestion_scopes, scope_generations,
// and fact_work_items in a disposable bootstrapped database.
type reconciliationQueueLiveFixture struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	asOf     time.Time
}

func newReconciliationQueueLiveFixture(t *testing.T) *reconciliationQueueLiveFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip(reconciliationQueueLiveSkip)
	}
	// Disposable database (#9299): the lane's public-qualified SECURITY
	// DEFINER bodies cannot install into an isolated schema; isolation comes
	// from the database, so no per-test schema prefix is needed.
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute, func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	return &reconciliationQueueLiveFixture{t: t, ctx: ctx, database: database, asOf: time.Now().UTC().Truncate(time.Second)}
}

func (f *reconciliationQueueLiveFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.database.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *reconciliationQueueLiveFixture) scope(scope string) {
	f.t.Helper()
	f.exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
VALUES ($1, 'repository', 'github', $1, 'security_alert', $1, $2, $2, 'active')
ON CONFLICT (scope_id) DO NOTHING`, scope, f.asOf)
	f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'schedule', $3, $3, 'active')
ON CONFLICT (generation_id) DO NOTHING`, scope+"-gen", scope, f.asOf)
}

func (f *reconciliationQueueLiveFixture) item(id, scope, domain, status string) {
	f.t.Helper()
	// A claimed or running row holds the conflict lease for its key, so each
	// such row gets its own conflict_key.
	f.exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, conflict_key, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, $5, $1, $6, $6)`, id, scope, scope+"-gen", domain, status, f.asOf)
}

func (f *reconciliationQueueLiveFixture) store() PostgresSecurityAlertCollectionStateStore {
	return NewPostgresSecurityAlertCollectionStateStoreWithReadStore(storagepostgres.SQLDB{DB: f.database})
}

// TestSecurityAlertReconciliationQueueStatesLive proves the per-scope queue
// read (#8839) against the real schema: pending, claimed, running, and
// retrying rows count as outstanding, dead_letter rows as dead-lettered, and
// succeeded, failed, and other-domain rows are excluded. A scope with only
// excluded rows is omitted from the result.
func TestSecurityAlertReconciliationQueueStatesLive(t *testing.T) {
	f := newReconciliationQueueLiveFixture(t)
	f.scope("scope-a")
	f.scope("scope-b")
	f.scope("scope-drained")
	const domain = "security_alert_reconciliation"
	f.item("wi-a-pending", "scope-a", domain, "pending")
	f.item("wi-a-claimed", "scope-a", domain, "claimed")
	f.item("wi-a-running", "scope-a", domain, "running")
	f.item("wi-a-retrying", "scope-a", domain, "retrying")
	f.item("wi-a-dead", "scope-a", domain, "dead_letter")
	f.item("wi-a-failed", "scope-a", domain, "failed")
	f.item("wi-a-done", "scope-a", domain, "succeeded")
	f.item("wi-a-other", "scope-a", "supply_chain_impact", "pending")
	f.item("wi-b-pending", "scope-b", domain, "pending")
	f.item("wi-drained-done", "scope-drained", domain, "succeeded")

	states, err := f.store().SecurityAlertReconciliationQueueStates(f.ctx, []string{"scope-a", "scope-b", "scope-drained", "scope-absent"})
	if err != nil {
		t.Fatalf("SecurityAlertReconciliationQueueStates: %v", err)
	}
	byScope := make(map[string]supplychain.SecurityAlertReconciliationQueueState, len(states))
	for _, state := range states {
		byScope[state.ScopeID] = state
	}
	if got := byScope["scope-a"]; got.Outstanding != 4 || got.DeadLetter != 1 {
		t.Fatalf("scope-a = %+v, want outstanding=4 dead_letter=1", got)
	}
	if got := byScope["scope-b"]; got.Outstanding != 1 || got.DeadLetter != 0 {
		t.Fatalf("scope-b = %+v, want outstanding=1 dead_letter=0", got)
	}
	if _, ok := byScope["scope-drained"]; ok {
		t.Fatalf("scope-drained reported %+v, want it omitted", byScope["scope-drained"])
	}
	if len(states) != 2 {
		t.Fatalf("reported %d scopes, want 2", len(states))
	}
}

// TestSecurityAlertReconciliationQueuePlanLive records EXPLAIN (ANALYZE,
// BUFFERS) for the shipped per-scope queue query over a 500,000-item
// security_alert_reconciliation backlog (#8839 acceptance criterion). Plans
// and timings are logged, not gated; the figures feed
// docs/internal/evidence/8839-security-alert-reconciliation-queue.md.
func TestSecurityAlertReconciliationQueuePlanLive(t *testing.T) {
	f := newReconciliationQueueLiveFixture(t)
	f.scope("scope-hot")
	f.scope("scope-quiet")
	f.exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
SELECT 'wi-hot-' || g, 'scope-hot', 'scope-hot-gen', 'reducer', 'security_alert_reconciliation',
       CASE WHEN g % 1000 = 0 THEN 'dead_letter' WHEN g % 7 = 0 THEN 'retrying' ELSE 'pending' END,
       $1, $1
FROM generate_series(1, 500000) AS g`, f.asOf)
	f.exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
SELECT 'wi-quiet-' || g, 'scope-quiet', 'scope-quiet-gen', 'reducer', 'security_alert_reconciliation', 'succeeded', $1, $1
FROM generate_series(1, 1000) AS g`, f.asOf)
	f.exec(`VACUUM ANALYZE fact_work_items`)

	absent := make([]string, 0, 1022)
	for i := 1; i <= 1022; i++ {
		absent = append(absent, "scope-absent")
	}
	for _, scopes := range [][]string{{"scope-hot"}, append([]string{"scope-hot", "scope-quiet"}, absent...)} {
		// The measured statement derives from the shipped query constant so
		// the plan cannot drift false-green when the constant changes.
		explainSQL := "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)\n" + securityAlertReconciliationQueueStatesQuery
		if strings.Count(explainSQL, securityAlertReconciliationQueueStatesQuery) != 1 {
			t.Fatalf("explain statement does not embed the shipped query constant verbatim")
		}
		rows, err := f.database.QueryContext(f.ctx, explainSQL, array.Of(scopes))
		if err != nil {
			t.Fatalf("explain for %d scopes: %v", len(scopes), err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				t.Fatalf("scan plan: %v", err)
			}
			lines = append(lines, line)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate plan: %v", err)
		}
		timing := ""
		for _, line := range lines {
			if strings.Contains(line, "Execution Time:") {
				timing = strings.TrimSpace(line)
			}
		}
		states, err := f.store().SecurityAlertReconciliationQueueStates(f.ctx, scopes)
		if err != nil {
			t.Fatalf("read for %d scopes: %v", len(scopes), err)
		}
		if len(states) != 1 || states[0].ScopeID != "scope-hot" {
			t.Fatalf("reported %d scopes, want only scope-hot", len(states))
		}
		t.Logf("%d scopes: %s (outstanding=%d dead_letter=%d)\n%s", len(scopes), timing, states[0].Outstanding, states[0].DeadLetter, strings.Join(lines, "\n"))
	}
}
