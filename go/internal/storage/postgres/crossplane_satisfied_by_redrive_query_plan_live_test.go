// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestCrossplaneRedriveTargetDiscoveryQueryPlanLive is the committed
// theory-proof for issue #5476's partial index
// (fact_records_active_k8s_claim_redrive_idx): it seeds a representative
// worst-case corpus (many scopes, many K8sResource claim facts, a small
// minority matching the XRD's (group, kind)) and proves the target-discovery
// query is index-backed and bounded, never a full fact_records scan, without
// changing the answer.
//
// The OLD (no usable index) shape is reproduced by disabling index and
// bitmap scans for the SAME query on the SAME data within a rolled-back
// transaction -- the standard, non-destructive way to compare plans with and
// without an index present on a schema other tests may share, rather than
// dropping and recreating the real index. See the PR body for the full
// EXPLAIN (ANALYZE, BUFFERS) text this test's t.Logf output reproduces.
func TestCrossplaneRedriveTargetDiscoveryQueryPlanLive(t *testing.T) {
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `SET synchronous_commit = off`); err != nil {
		t.Fatalf("relax synchronous_commit: %v", err)
	}

	now := time.Now().UTC()
	const (
		numScopes      = 4000
		claimsPerScope = 50
		matchingScopes = 60 // ~1.5% match the XRD's (group, kind) -- the minority a real corpus expects
		xrdScopeID     = "scope-xrd-platform"
		xrdGenID       = "gen-xrd-platform-001"
		matchGroup     = "example.org"
		matchKind      = "XExampleClaim"
	)

	seedCrossplaneRedriveXRD(ctx, t, conn, xrdScopeID, xrdGenID, matchGroup, matchKind, now)
	for i := 0; i < numScopes; i++ {
		group, kind := matchGroup, matchKind
		if i >= matchingScopes {
			group = fmt.Sprintf("noise-%d.example.org", i%997)
			kind = fmt.Sprintf("NoiseKind%d", i%53)
		}
		seedCrossplaneRedriveClaimScope(ctx, t, conn, fmt.Sprintf("scope-claim-%05d", i), fmt.Sprintf("gen-claim-%05d-001", i), group, kind, claimsPerScope, now)
	}
	if _, err := conn.ExecContext(ctx, `ANALYZE fact_records; ANALYZE ingestion_scopes; ANALYZE scope_generations; ANALYZE fact_work_items;`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	explain := func(label string, disableIndex bool) string {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin explain tx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		if disableIndex {
			if _, err := tx.ExecContext(ctx, `SET LOCAL enable_indexscan = off`); err != nil {
				t.Fatalf("disable indexscan: %v", err)
			}
			if _, err := tx.ExecContext(ctx, `SET LOCAL enable_bitmapscan = off`); err != nil {
				t.Fatalf("disable bitmapscan: %v", err)
			}
		}
		rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+listCrossplaneRedriveTargetScopesQuery,
			matchGroup, matchKind, xrdScopeID, "", 500)
		if err != nil {
			t.Fatalf("%s: explain query: %v", label, err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if scanErr := rows.Scan(&line); scanErr != nil {
				_ = rows.Close()
				t.Fatalf("scan explain line: %v", scanErr)
			}
			plan.WriteString(line)
			plan.WriteString("\n")
		}
		_ = rows.Close()
		t.Logf("=== %s ===\n%s", label, plan.String())
		return plan.String()
	}

	oldPlan := explain("OLD (indexscan/bitmapscan disabled -- pre-#5476 shape)", true)
	if !strings.Contains(oldPlan, "Seq Scan on fact_records") {
		t.Fatalf("expected OLD plan to fall back to a sequential scan of fact_records:\n%s", oldPlan)
	}

	newPlan := explain("NEW (fact_records_active_k8s_claim_redrive_idx)", false)
	if !strings.Contains(newPlan, "fact_records_active_k8s_claim_redrive_idx") {
		t.Fatalf("expected NEW plan to use fact_records_active_k8s_claim_redrive_idx:\n%s", newPlan)
	}
	if strings.Contains(newPlan, "Seq Scan on fact_records") {
		t.Fatalf("expected NEW plan to avoid a sequential scan of fact_records:\n%s", newPlan)
	}

	// Row-set equivalence: the indexed query must return EXACTLY the same
	// target scopes as the naive (index-disabled) shape -- the index may only
	// change the plan, never the answer.
	naive := collectCrossplaneRedriveTargetScopes(ctx, t, conn, true, matchGroup, matchKind, xrdScopeID)
	indexed := collectCrossplaneRedriveTargetScopes(ctx, t, conn, false, matchGroup, matchKind, xrdScopeID)
	onlyInNaive, onlyInIndexed := crossplaneRedriveSymmetricDiff(naive, indexed)
	t.Logf("naive=%d indexed=%d symmetric_diff_naive_only=%d symmetric_diff_indexed_only=%d",
		len(naive), len(indexed), len(onlyInNaive), len(onlyInIndexed))
	if len(onlyInNaive) != 0 || len(onlyInIndexed) != 0 {
		t.Fatalf("row-set mismatch between naive and indexed plans: naive_only=%v indexed_only=%v", onlyInNaive, onlyInIndexed)
	}
	if len(indexed) != matchingScopes {
		t.Fatalf("expected %d matching target scopes, got %d", matchingScopes, len(indexed))
	}
}

// crossplaneRedriveExecer is the minimal write surface the seed helpers need.
// Both *sql.Conn (a single checked-out connection, needed by the query-plan
// proof so EXPLAIN and the seed data share one session/planner cache) and
// *sql.DB (needed by the behavior proof, which must NOT hold a connection
// checked out while the handler/sweeper under test also draw from the same
// pool) satisfy it.
type crossplaneRedriveExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func seedCrossplaneRedriveXRD(ctx context.Context, t *testing.T, conn crossplaneRedriveExecer, scopeID, generationID, group, claimKind string, now time.Time) {
	t.Helper()
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1,'repository','git',$1,'git','p1',$2,$2,'active',$3,'{}'::jsonb)
		ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id
	`, scopeID, now, generationID)
	// Supersede any PRIOR 'active' generation for this scope before
	// activating a new one: scope_generations_active_scope_idx is a unique
	// partial index on (scope_id) WHERE status = 'active', so a scope
	// activating a SECOND generation (e.g. a repeat-sync test) without first
	// retiring the old 'active' row would violate that constraint -- exactly
	// what a real resync's Ack does via supersedeProjectorActiveGenerationQuery.
	crossplaneRedriveMustExec(ctx, t, conn, `
		UPDATE scope_generations
		SET status = 'superseded'
		WHERE scope_id = $1 AND generation_id <> $2 AND status = 'active'
	`, scopeID, generationID)
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
		VALUES ($1,$2,'sync',$3,$3,'active',$3,'{}'::jsonb)
		ON CONFLICT (generation_id) DO NOTHING
	`, generationID, scopeID, now)
	// fact_id is keyed by (scopeID, generationID), not a hardcoded literal:
	// a hardcoded fact_id would silently no-op (ON CONFLICT DO NOTHING) when
	// the SAME xrd scope activates a SECOND generation (e.g. a repeat sync
	// test), since fact_records.fact_id is a global primary key.
	factID := "fact-xrd-" + generationID
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		VALUES ($1,$2,$3,'content_entity',$1,'git',$1,$4,$4,FALSE,jsonb_build_object('entity_type','CrossplaneXRD','entity_id',$1 || '-uid','entity_metadata', jsonb_build_object('group', $5::text, 'claim_kind', $6::text)))
		ON CONFLICT (fact_id) DO NOTHING
	`, factID, scopeID, generationID, now, group, claimKind)
}

func seedCrossplaneRedriveClaimScope(ctx context.Context, t *testing.T, conn crossplaneRedriveExecer, scopeID, generationID, group, kind string, claimsPerScope int, now time.Time) {
	t.Helper()
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1,'repository','git',$1,'git','p1',$2,$2,'active',$3,'{}'::jsonb)
		ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id
	`, scopeID, now, generationID)
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
		VALUES ($1,$2,'sync',$3,$3,'active',$3,'{}'::jsonb)
		ON CONFLICT (generation_id) DO NOTHING
	`, generationID, scopeID, now)
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		SELECT
		    $1 || '-' || d::text, $2, $3, 'content_entity', $1 || '-' || d::text, 'git', $1 || '-' || d::text, $4, $4, FALSE,
		    jsonb_build_object('entity_type','K8sResource','entity_id', $1 || '-uid-' || d::text, 'entity_metadata', jsonb_build_object('api_version', $5::text || '/v1', 'kind', $6::text))
		FROM generate_series(0, $7::int-1) AS d
		ON CONFLICT (fact_id) DO NOTHING
	`, scopeID, scopeID, generationID, now, group, kind, claimsPerScope)
}

func collectCrossplaneRedriveTargetScopes(
	ctx context.Context, t *testing.T, conn *sql.Conn, disableIndex bool,
	group, kind, xrdScopeID string,
) map[string]struct{} {
	t.Helper()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if disableIndex {
		if _, err := tx.ExecContext(ctx, `SET LOCAL enable_indexscan = off`); err != nil {
			t.Fatalf("disable indexscan: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `SET LOCAL enable_bitmapscan = off`); err != nil {
			t.Fatalf("disable bitmapscan: %v", err)
		}
	}
	out := make(map[string]struct{})
	after := ""
	const pageSize = 500
	for {
		rows, err := tx.QueryContext(ctx, listCrossplaneRedriveTargetScopesQuery, group, kind, xrdScopeID, after, pageSize)
		if err != nil {
			t.Fatalf("query target scopes: %v", err)
		}
		count := 0
		var last string
		for rows.Next() {
			var scopeID, genID string
			if scanErr := rows.Scan(&scopeID, &genID); scanErr != nil {
				_ = rows.Close()
				t.Fatalf("scan target scope: %v", scanErr)
			}
			out[scopeID] = struct{}{}
			last = scopeID
			count++
		}
		_ = rows.Close()
		if count < pageSize {
			break
		}
		after = last
	}
	return out
}

func crossplaneRedriveSymmetricDiff(a, b map[string]struct{}) (onlyA, onlyB []string) {
	for k := range a {
		if _, ok := b[k]; !ok {
			onlyA = append(onlyA, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			onlyB = append(onlyB, k)
		}
	}
	return onlyA, onlyB
}

func crossplaneRedriveMustExec(ctx context.Context, t *testing.T, conn crossplaneRedriveExecer, query string, args ...any) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
