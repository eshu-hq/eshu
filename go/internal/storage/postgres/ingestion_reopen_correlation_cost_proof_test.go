// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

// scopesForCorrelationReopenCostProof and generationsForCorrelationReopenCostProof
// size the worst-case corpus the cost proof measures: a long-lived store where
// every scope has been re-ingested 25 times.
const (
	scopesForCorrelationReopenCostProof      = 900
	generationsForCorrelationReopenCostProof = 25
)

// TestCorrelationReopenPerDrainCostProof measures the REAL per-drain cost of the
// cross-scope correlation reopen the ingester now runs, including the client
// round-trips production actually issues.
//
// It exists because the earlier evidence for this change measured only the
// listing SELECT and a server-side UPDATE loop, which is not the production
// shape: ReopenSucceededReducerWorkItems issues ONE client round-trip per
// reopened row (queue.ReopenSucceeded in a loop), and the pre-change ingester
// baseline for this work was ZERO — the maintenance pass did none of it. A
// cost stated as a delta against a hypothetical unbounded variant hides that.
//
// What this proof measures:
//
//   - the whole production call, five domains, listing plus one round-trip per
//     reopened row, against a real Postgres;
//   - the same call with nothing left to reopen (listings only), which isolates
//     the listing cost from the round-trip cost;
//   - the same call with an UNBOUNDED listing, which is what the pass would cost
//     without the per-scope replay floor.
//
// What it does NOT measure, and what no shim here can: the downstream cost of
// the reducer RE-EXECUTING every reopened work item. Each drain hands the
// reducer one item per active scope per domain, forever; two of the five
// domains (deployable_unit_correlation,
// kubernetes_correlation_materialization) write GRAPH EDGES when they run. That
// steady-state queue pressure is the dominant real cost of this change and is
// bounded only by the replay floor keeping the count at O(active scopes) rather
// than O(active scopes x generations).
//
// Gated on its own DSN variable rather than the shared proof DSN because the
// unbounded arm deliberately issues 112 500 round-trips and takes minutes.
func TestCorrelationReopenPerDrainCostProof(t *testing.T) {
	dsn := os.Getenv("ESHU_CORRELATION_REOPEN_COST_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_CORRELATION_REOPEN_COST_PROOF_DSN to run the correlation-reopen per-drain cost proof")
	}
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)
	seedCorrelationReopenCostCorpus(t, ctx, db)

	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC) }
	domains := CrossScopeCorrelationReopenDomains()

	coldPass := timeCorrelationReopen(t, ctx, store, domains)
	reopened := countWorkItems(t, ctx, db, "pending")

	// Nothing left in 'succeeded': the listings run and match no rows, so this
	// isolates the five listings from the round-trips.
	listingsOnly := timeCorrelationReopen(t, ctx, store, domains)

	// Steady state: the reducer re-succeeds everything it was handed, then the
	// next drain's maintenance pass runs again.
	resucceedCorrelationReopenCostCorpus(t, ctx, db)
	steadyState := timeCorrelationReopen(t, ctx, store, domains)

	resucceedCorrelationReopenCostCorpus(t, ctx, db)
	unbounded, unboundedRows := timeUnboundedCorrelationReopen(t, ctx, db, store, domains)

	t.Logf("corpus = %d succeeded work items (%d scopes x %d generations x %d domains)",
		countWorkItems(t, ctx, db, ""),
		scopesForCorrelationReopenCostProof, generationsForCorrelationReopenCostProof, len(domains))
	t.Logf("bounded cold pass      = %s, %d rows reopened", coldPass.Round(time.Millisecond), reopened)
	t.Logf("listings only (0 rows) = %s", listingsOnly.Round(time.Millisecond))
	t.Logf("bounded steady state   = %s", steadyState.Round(time.Millisecond))
	t.Logf("UNBOUNDED pass         = %s, %d rows reopened",
		unbounded.Round(time.Millisecond), unboundedRows)

	if reopened != scopesForCorrelationReopenCostProof*len(domains) {
		t.Fatalf("bounded pass reopened %d rows, want %d (one per active scope per domain)",
			reopened, scopesForCorrelationReopenCostProof*len(domains))
	}
	if unboundedRows <= reopened {
		t.Fatalf("unbounded pass reopened %d rows, want more than the bounded %d", unboundedRows, reopened)
	}
}

func timeCorrelationReopen(
	t *testing.T,
	ctx context.Context,
	store IngestionStore,
	domains []string,
) time.Duration {
	t.Helper()
	start := time.Now()
	if err := store.ReopenSucceededReducerWorkItems(ctx, nil, nil, domains); err != nil {
		t.Fatalf("ReopenSucceededReducerWorkItems() error = %v", err)
	}
	return time.Since(start)
}

// timeUnboundedCorrelationReopen runs the same production loop against a listing
// with NO replay floor, which is what the pass would cost unbounded.
func timeUnboundedCorrelationReopen(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	store IngestionStore,
	domains []string,
) (time.Duration, int) {
	t.Helper()
	queue := ReducerQueue{db: SQLDB{DB: db}, Now: store.Now}
	start := time.Now()
	reopened := 0
	for _, domain := range domains {
		rows, err := db.QueryContext(ctx, `
SELECT work_item_id FROM fact_work_items
WHERE stage = 'reducer' AND domain = $1 AND status = 'succeeded'
ORDER BY updated_at ASC, work_item_id ASC`, domain)
		if err != nil {
			t.Fatalf("unbounded listing for %s: %v", domain, err)
		}
		ids := make([]string, 0)
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan unbounded listing row: %v", err)
			}
			ids = append(ids, id)
		}
		_ = rows.Close()
		for _, id := range ids {
			if _, err := queue.ReopenSucceeded(ctx, id); err != nil {
				t.Fatalf("unbounded reopen of %s: %v", id, err)
			}
		}
		reopened += len(ids)
	}
	return time.Since(start), reopened
}

func seedCorrelationReopenCostCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE INDEX fact_work_items_stage_domain_status_idx
		   ON fact_work_items (stage, domain, status, visible_at, updated_at DESC)`,
		`CREATE INDEX fact_work_items_scope_generation_idx
		   ON fact_work_items (scope_id, generation_id, status, updated_at DESC)`,
		fmt.Sprintf(`INSERT INTO ingestion_scopes (scope_id, active_generation_id)
		   SELECT 'scope-' || s, NULL FROM generate_series(1, %d) AS s`,
			scopesForCorrelationReopenCostProof),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, ingested_at)
		   SELECT 'gen-' || s || '-' || g, 'scope-' || s,
		          TIMESTAMPTZ '2026-07-01 00:00:00+00' + (g || ' hours')::interval
		   FROM generate_series(1, %d) AS s, generate_series(1, %d) AS g`,
			scopesForCorrelationReopenCostProof, generationsForCorrelationReopenCostProof),
		fmt.Sprintf(`UPDATE ingestion_scopes
		   SET active_generation_id = 'gen-' || split_part(scope_id, '-', 2) || '-%d'`,
			generationsForCorrelationReopenCostProof),
	}
	for _, domain := range CrossScopeCorrelationReopenDomains() {
		statements = append(statements, fmt.Sprintf(`
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at)
SELECT 'wi-%s-' || s || '-' || g, 'scope-' || s, 'gen-' || s || '-' || g,
       'reducer', '%s', 'succeeded', '{}'::jsonb,
       TIMESTAMPTZ '2026-07-01 00:00:00+00' + (g || ' hours')::interval,
       TIMESTAMPTZ '2026-07-01 00:00:00+00' + (g || ' hours')::interval
FROM generate_series(1, %d) AS s, generate_series(1, %d) AS g`,
			domain, domain,
			scopesForCorrelationReopenCostProof, generationsForCorrelationReopenCostProof))
	}
	statements = append(statements, "ANALYZE")
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed correlation reopen cost corpus: %v", err)
		}
	}
}

func resucceedCorrelationReopenCostCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(
		ctx, "UPDATE fact_work_items SET status = 'succeeded' WHERE status = 'pending'",
	); err != nil {
		t.Fatalf("re-succeed reopened work items: %v", err)
	}
}

func countWorkItems(t *testing.T, ctx context.Context, db *sql.DB, status string) int {
	t.Helper()
	query := "SELECT count(*) FROM fact_work_items"
	args := make([]any, 0, 1)
	if status != "" {
		query += " WHERE status = $1"
		args = append(args, status)
	}
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("count work items: %v", err)
	}
	return count
}
