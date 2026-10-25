// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8954 disposable PostgreSQL 18 proofs that the status blockage read names the domain holding a blocked row's conflict fence (a claimed supply_chain_impact row fencing a pending security_alert_reconciliation row) and that the holder lookup reads the live-lease set once with and without planner statistics; each proof opens its own schema from the bootstrap migrations and drops it; about 1 s per proof on PostgreSQL 18.6 locally; wall time is not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

// seedHolderFenceFixture seeds one active scope whose reducer queue holds the
// #8827 shape: a live claimed supply_chain_impact row and a pending
// security_alert_reconciliation row, both on the default scope fence (conflict
// domain "scope", conflict_key NULL so the fence key is COALESCE(conflict_key,
// scope_id)). A second scope holds a pending row blocked only by a missing
// readiness phase, so it has no holder.
func seedHolderFenceFixture(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}
	asOf := statusSemanticsAsOf
	for _, scope := range []string{"s-fence", "s-ready"} {
		exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $1 || '-g', '{}'::jsonb)`, scope, asOf.Add(-time.Hour))
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at,
  ingested_at, status, payload)
VALUES ($1 || '-g', $1, 'snapshot', $2, $2, 'active', '{}'::jsonb)`, scope, asOf.Add(-time.Hour))
	}
	for _, work := range []struct {
		id, scope, domain, status, conflictDomain string
		claimUntil                                *time.Duration
	}{
		{"w-impact", "s-fence", "supply_chain_impact", "claimed", "scope", durationPtr(10 * time.Minute)},
		{"w-reconcile", "s-fence", "security_alert_reconciliation", "pending", "scope", nil},
		{"w-ready", "s-ready", "aws_relationship_materialization", "pending", "scope", nil},
	} {
		var claimUntil any
		if work.claimUntil != nil {
			claimUntil = asOf.Add(*work.claimUntil)
		}
		exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  conflict_domain, conflict_key, status, attempt_count, claim_until, payload, created_at, updated_at)
VALUES ($1, $2, $2 || '-g', 'reducer', $3, $4, NULL, $5, 0, $6, '{}'::jsonb, $7, $7)`,
			work.id, work.scope, work.domain, work.conflictDomain, work.status, claimUntil, asOf.Add(-30*time.Minute))
	}
	exec("ANALYZE")
}

// TestReducerConflictBlockageNamesHolderDomain is the #8954 regression: a
// reconciliation fenced by a claimed supply_chain_impact row reports
// supply_chain_impact as the holder, from the same summary read, and a row
// blocked only by a readiness requirement reports no holder.
func TestReducerConflictBlockageNamesHolderDomain(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #8954 holder-domain proof")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedHolderFenceFixture(ctx, t, conn)

	summary, err := readActiveWorkSummary(ctx, sqlConnQueryer{conn: conn}, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("readActiveWorkSummary() error = %v", err)
	}
	holders := map[string]string{}
	for _, row := range summary.Blockages {
		holders[row.Domain+"|"+row.ConflictDomain] = row.HolderDomain
	}
	if got, want := holders["security_alert_reconciliation|scope"], "supply_chain_impact"; got != want {
		t.Fatalf("reconciliation HolderDomain = %q, want %q; blockages=%+v", got, want, summary.Blockages)
	}
	if got := holders["aws_relationship_materialization|readiness"]; got != "" {
		t.Fatalf("readiness-blocked HolderDomain = %q, want empty (no lease holds it); blockages=%+v", got, summary.Blockages)
	}
	if _, ok := holders["aws_relationship_materialization|readiness"]; !ok {
		t.Fatalf("readiness blockage row missing; blockages=%+v", summary.Blockages)
	}
	if _, ok := holders["supply_chain_impact|scope"]; ok {
		t.Fatalf("the holder is not itself blocked; blockages=%+v", summary.Blockages)
	}
}

// TestActiveWorkSummaryHolderLookupScansLeasesOnce is the #8954 plan regression:
// with no statistics and with fresh ones, over a backlog with live leases, the
// holder join reads the materialized lease set once, so the added column costs
// one pass over the live leases, not one per blocked row.
func TestActiveWorkSummaryHolderLookupScansLeasesOnce(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #8954 holder plan regression")
	}
	ctx := context.Background()
	conn := openUnanalyzedBlockageSchema(ctx, t, dsn)
	seedUnanalyzedBlockageBacklog(ctx, t, conn, 200, 20, 100)
	requireNoStatistics(ctx, t, conn)
	if err := requireLeaseScansRunOnce(explainAnalyzeJSON(ctx, t, conn, activeWorkSummaryQuery)); err != nil {
		t.Fatalf("no statistics: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "ANALYZE ingestion_scopes, scope_generations, fact_work_items"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if err := requireLeaseScansRunOnce(explainAnalyzeJSON(ctx, t, conn, activeWorkSummaryQuery)); err != nil {
		t.Fatalf("analyzed: %v", err)
	}
}
