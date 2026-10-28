// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9320 PostgreSQL 18.6 plan proof of migration 245: the shipped shared_projection_pending aggregate, derived from domainBacklogCTEs, reads MIN(COALESCE(reopened_at, created_at)) over 12,000 vacuumed intents with 1,500 pending across 4 domains as an Index Only Scan on shared_projection_intents_domain_pending_age_idx with Heap Fetches 0, and loses that plan when the index is dropped, so the index is load-bearing; about 2-3 s locally in an isolated schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// sharedProjectionPendingCTEBody returns the body of the shipped
// shared_projection_pending CTE, cut from domainBacklogCTEs so the plan proof
// runs the exact production text.
func sharedProjectionPendingCTEBody(t *testing.T) string {
	t.Helper()
	const head = "shared_projection_pending AS ("
	start := strings.Index(domainBacklogCTEs, head)
	if start < 0 {
		t.Fatalf("domainBacklogCTEs has no %q", head)
	}
	body := domainBacklogCTEs[start+len(head):]
	depth := 1
	for i, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return body[:i]
			}
		}
	}
	t.Fatal("shared_projection_pending CTE is not closed")
	return ""
}

// TestSharedIntentPendingAgeIndexOnlyScanLive proves migration 245 keeps the
// reopen-aware shared-projection backlog aggregate off the heap (#9320).
// Without the index the planner must read reopened_at from the heap of every
// block that holds a pending intent; with it the aggregate is answered from
// the partial index alone.
func TestSharedIntentPendingAgeIndexOnlyScanLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "spi_age_plan", "set ESHU_POSTGRES_DSN to run the #9320 shared-intent age plan proof")
	query := sharedProjectionPendingCTEBody(t)
	if !strings.Contains(query, "MIN(COALESCE(reopened_at, created_at))") {
		t.Fatalf("shared_projection_pending does not read the reopen-aware age:\n%s", query)
	}

	now := time.Now().UTC()
	// 12,000 intents over 4 domains; 1,500 are pending, spread over every
	// domain (g / 4 groups one id from each domain), and every third pending
	// group carries a reopen stamp.
	if _, err := database.ExecContext(ctx, `INSERT INTO shared_projection_intents (intent_id, projection_domain,
  partition_key, scope_id, acceptance_unit_id, repository_id, source_run_id, generation_id, payload,
  created_at, completed_at, reopened_at)
SELECT 'i-' || g, 'domain_' || (g % 4), 'p' || g, 's' || (g % 50), 'u' || g, 'r' || (g % 50), 'run', 'g' || (g % 50),
  '{}'::jsonb, $1::timestamptz - make_interval(secs => g),
  CASE WHEN (g / 4) % 8 = 0 THEN NULL ELSE $1::timestamptz END,
  CASE WHEN (g / 4) % 24 = 0 THEN $1::timestamptz - make_interval(secs => g / 2) END
FROM generate_series(1, 12000) AS g`, now); err != nil {
		t.Fatalf("seed intents: %v", err)
	}
	waitAllVisible(ctx, t, database, "shared_projection_intents")

	explain := func() string {
		t.Helper()
		rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) "+query)
		if err != nil {
			t.Fatalf("explain shared_projection_pending: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan plan: %v", err)
			}
			plan.WriteString(line + "\n")
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read plan: %v", err)
		}
		return plan.String()
	}

	withIndex := explain()
	if !strings.Contains(withIndex, "rows=4.00") {
		t.Fatalf("seed must spread pending intents over 4 domains:\n%s", withIndex)
	}
	if !strings.Contains(withIndex, "Index Only Scan using shared_projection_intents_domain_pending_age_idx") ||
		!strings.Contains(withIndex, "Heap Fetches: 0") {
		t.Fatalf("with migration 245 the aggregate must be an Index Only Scan on "+
			"shared_projection_intents_domain_pending_age_idx with Heap Fetches 0:\n%s", withIndex)
	}

	// Control: without the index the same text must leave the index-only path,
	// or the assertion above would pass whatever migration 245 does.
	if _, err := database.ExecContext(ctx, `DROP INDEX shared_projection_intents_domain_pending_age_idx`); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	withoutIndex := explain()
	if strings.Contains(withoutIndex, "Index Only Scan") {
		t.Fatalf("without migration 245 the aggregate still found an index-only path, so the proof is vacuous:\n%s", withoutIndex)
	}
}

// waitAllVisible vacuums table until its visibility map covers every page, so
// an index-only scan can skip the heap. VACUUM cannot mark a page all-visible
// while another backend in the database holds a snapshot older than the
// page's rows; a parallel live test in the same database does that for a few
// seconds at a time, so the wait is bounded and names the holder on timeout.
func waitAllVisible(ctx context.Context, t *testing.T, database *sql.DB, table string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := database.ExecContext(ctx, "VACUUM ANALYZE "+table); err != nil {
			t.Fatalf("vacuum analyze %s: %v", table, err)
		}
		var pages, allVisible int
		if err := database.QueryRowContext(ctx, `SELECT relpages, relallvisible FROM pg_class
WHERE oid = $1::regclass`, table).Scan(&pages, &allVisible); err != nil {
			t.Fatalf("read visibility map coverage of %s: %v", table, err)
		}
		if pages > 0 && allVisible == pages {
			return
		}
		if time.Now().After(deadline) {
			var holder string
			_ = database.QueryRowContext(ctx, `SELECT COALESCE(string_agg(pid || ' ' || state || ' ' || left(query, 80), '; '), 'none')
FROM pg_stat_activity WHERE backend_xmin IS NOT NULL AND pid <> pg_backend_pid()`).Scan(&holder)
			t.Fatalf("%s visibility map covers %d of %d pages after 60 s; an older snapshot blocks VACUUM: %s",
				table, allVisible, pages, holder)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
