// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhookstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestWebhookTriggerStoreReapPlanUsesClaimedAtIndex proves the #7661 sweep
// statements read the partial webhook_refresh_triggers_claimed_at_idx,
// not the table: on 10,000 handed_off rows plus 300 stale claimed rows,
// none of the reap, exhaust, or stuck-count plans may contain a Seq Scan
// on webhook_refresh_triggers.
func TestWebhookTriggerStoreReapPlanUsesClaimedAtIndex(t *testing.T) {
	dsn := os.Getenv("ESHU_WEBHOOK_CLAIM_LEASE_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_WEBHOOK_CLAIM_LEASE_PROOF_DSN to run the webhook claim-lease plan proof")
	}
	ctx := context.Background()
	db := openClaimLeaseExplainDB(t, ctx, dsn)
	store := NewWebhookTriggerStore(postgres.SQLDB{DB: db})
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC).UTC()
	seedClaimLeaseExplainRows(t, ctx, db, now)
	if _, err := db.ExecContext(ctx, "ANALYZE webhook_refresh_triggers"); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	staleBefore := now.Add(-5 * time.Minute)
	// Token 5 on 50 rows exercises the exhaust arm's predicate; the rest
	// stay at token 1 under the cap of 3.
	if _, err := db.ExecContext(ctx, `UPDATE webhook_refresh_triggers SET claim_fencing_token = 5
WHERE status = 'claimed' AND trigger_id LIKE 'stale-%' AND substr(trigger_id, 7)::int <= 50`); err != nil {
		t.Fatalf("set exhaust tokens: %v", err)
	}

	for _, probe := range []struct {
		name  string
		query string
		args  []any
	}{
		{"reap", reapStaleWebhookTriggerClaimsQuery, []any{staleBefore, 3, 100, now}},
		{"exhaust", exhaustStaleWebhookTriggerClaimsQuery, []any{staleBefore, 3, 100, now}},
		{"count", countStaleWebhookTriggerClaimsQuery, []any{staleBefore}},
	} {
		plan, execMs := explainClaimLeaseQuery(t, ctx, db, probe.query, probe.args...)
		assertNoSeqScanOnTriggers(t, probe.name, plan)
		assertClaimedAtIndexUsed(t, probe.name, plan)
		t.Logf("%s: execution time %.3f ms", probe.name, execMs)
	}
}

func openClaimLeaseExplainDB(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	bootstrap, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof connection: %v", err)
	}
	defer func() { _ = bootstrap.Close() }()
	schemaName := fmt.Sprintf("webhook_claim_lease_plan_%d", time.Now().UnixNano())
	if _, err := bootstrap.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = bootstrap.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	db, err := sql.Open("pgx", dsn+separator+"options="+url.QueryEscape("-c search_path="+schemaName))
	if err != nil {
		t.Fatalf("open proof connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedClaimLeaseExplainRows(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
	t.Helper()
	// 10,000 handed_off history rows plus 300 stale claimed rows: the
	// issue's large-table/few-stale shape.
	if _, err := db.ExecContext(ctx, `INSERT INTO webhook_refresh_triggers (
    trigger_id, delivery_key, refresh_key, provider, event_kind, decision,
    delivery_id, repository_external_id, repository_full_name, default_branch,
    ref, target_sha, status, received_at, updated_at, handed_off_at, claim_fencing_token
) SELECT 'history-' || g, 'delivery-history-' || g, 'refresh-history-' || g,
    'github', 'push', 'accepted', 'delivery-history-' || g, 'repo', 'org/repo',
    'main', 'refs/heads/main', 'sha', 'handed_off', $1, $1, $1, 1
FROM generate_series(1, 10000) AS g`, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("seed history rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO webhook_refresh_triggers (
    trigger_id, delivery_key, refresh_key, provider, event_kind, decision,
    delivery_id, repository_external_id, repository_full_name, default_branch,
    ref, target_sha, status, received_at, updated_at, claimed_by, claimed_at, claim_fencing_token
) SELECT 'stale-' || g, 'delivery-stale-' || g, 'refresh-stale-' || g,
    'github', 'push', 'accepted', 'delivery-stale-' || g, 'repo', 'org/repo',
    'main', 'refs/heads/main', 'sha', 'claimed', $1, $1, 'owner-a', $1, 1
FROM generate_series(1, 300) AS g`, now.Add(-time.Hour)); err != nil {
		t.Fatalf("seed stale rows: %v", err)
	}
}

func explainClaimLeaseQuery(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) (map[string]any, float64) {
	t.Helper()
	var raw json.RawMessage
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	var explained []map[string]any
	if err := json.Unmarshal(raw, &explained); err != nil || len(explained) != 1 {
		t.Fatalf("decode EXPLAIN JSON: %v", err)
	}
	plan, _ := explained[0]["Plan"].(map[string]any)
	if plan == nil {
		t.Fatal("EXPLAIN JSON has no Plan")
	}
	execMs, _ := explained[0]["Execution Time"].(float64)
	return plan, execMs
}

func walkClaimLeasePlan(node map[string]any, visit func(map[string]any)) {
	visit(node)
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if childMap, ok := child.(map[string]any); ok {
			walkClaimLeasePlan(childMap, visit)
		}
	}
}

func assertNoSeqScanOnTriggers(t *testing.T, name string, plan map[string]any) {
	t.Helper()
	walkClaimLeasePlan(plan, func(node map[string]any) {
		if node["Node Type"] == "Seq Scan" && node["Relation Name"] == "webhook_refresh_triggers" {
			planJSON, _ := json.Marshal(plan)
			t.Fatalf("%s plan seq-scans webhook_refresh_triggers: %s", name, planJSON)
		}
	})
}

func assertClaimedAtIndexUsed(t *testing.T, name string, plan map[string]any) {
	t.Helper()
	used := false
	walkClaimLeasePlan(plan, func(node map[string]any) {
		if node["Index Name"] == "webhook_refresh_triggers_claimed_at_idx" {
			used = true
		}
	})
	if !used {
		planJSON, _ := json.Marshal(plan)
		t.Fatalf("%s plan does not use webhook_refresh_triggers_claimed_at_idx: %s", name, planJSON)
	}
}
