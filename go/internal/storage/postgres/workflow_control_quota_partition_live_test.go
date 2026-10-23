// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8834 real PostgreSQL quota partition proof: an exhausted component and scope pair does not block a different scope, and each scope grants its own limit; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Per-(component, scope) quota partitioning proofs (#8471).
//
// Split from workflow_control_quota_live_test.go before it approached the
// file cap: this file pins the partitioning invariant the docs state, that
// file pins the grant/race/restart/fallback behaviors.
//
// Run with a throwaway Postgres:
//
//	ESHU_POSTGRES_DSN='postgres://postgres:postgres@localhost:<port>/eshu?sslmode=disable' \
//	  go test ./internal/storage/postgres -run 'ClaimQuotaScopesPartition' -count=1 -v

// quotaScopeGrants reads one scope row's current-window grants, pinning the
// per-(component, scope) partitioning: one scope's budget never absorbs
// another's.
func quotaScopeGrants(t *testing.T, database *sql.DB, ctx context.Context, scopeID string) int {
	t.Helper()
	var grants sql.NullInt64
	err := database.QueryRowContext(ctx,
		`SELECT grants FROM component_quota_windows
		 WHERE component = $1 AND scope_id = $2 AND window_start = date_trunc('hour', now())`,
		string(scope.CollectorGit), scopeID).Scan(&grants)
	if err != nil {
		t.Fatalf("quota scope grants error = %v, want nil", err)
	}
	return int(grants.Int64)
}

// TestClaimQuotaScopesPartitionBudget pins the #8471 per-(component, scope)
// invariant the docs now state: each scope row independently allows up to
// the limit, so an exhausted scope neither blocks nor absorbs another
// scope's budget. Scope A is prefilled to the limit before any claim runs;
// scope B must still grant its full budget (a component-total gate would
// grant nothing here), and the window total must reach twice the limit.
func TestClaimQuotaScopesPartitionBudget(t *testing.T) {
	const limit = 2
	const scopeA = "quota-partition-scope-a"
	const scopeB = "quota-partition-scope-b"
	database, _ := openWorkflowControlIntegrationStore(t)
	store, ctx, reader := quotaTestFixture(t, database, limit, 60)
	base := time.Now().UTC().Truncate(time.Millisecond)
	seedQuotaBacklogInScope(t, store, ctx, "quota-partition-run", base, limit+1, scopeB)
	if _, err := database.ExecContext(ctx,
		`INSERT INTO component_quota_windows (component, scope_id, window_start, grants)
		 VALUES ($1, $2, date_trunc('hour', now()), $3)`,
		string(scope.CollectorGit), scopeA, limit); err != nil {
		t.Fatalf("prefill scope A quota error = %v, want nil", err)
	}

	for i := 1; i <= limit; i++ {
		item, _, found := quotaClaim(t, store, ctx, "quota-partition-owner", i)
		if !found {
			t.Fatalf("scope B claim %d found = false, want true (scope A exhaustion must not block scope B)", i)
		}
		if item.ScopeID != scopeB {
			t.Fatalf("granted scope = %q, want %q (only scope B has budget)", item.ScopeID, scopeB)
		}
	}
	if _, _, found := quotaClaim(t, store, ctx, "quota-partition-owner", limit+1); found {
		t.Fatal("over-budget scope B claim found = true, want false (scope B budget exhausted)")
	}
	if got := quotaScopeGrants(t, database, ctx, scopeA); got != limit {
		t.Fatalf("scope A grants = %d, want %d (untouched by scope B claims)", got, limit)
	}
	if got := quotaScopeGrants(t, database, ctx, scopeB); got != limit {
		t.Fatalf("scope B grants = %d, want %d (full per-scope budget)", got, limit)
	}
	if got := quotaWindowGrants(t, database, ctx); got != 2*limit {
		t.Fatalf("window grants = %d, want %d (per-scope budgets multiply across scopes)", got, 2*limit)
	}
	if got := quotaCounterTotal(t, reader, "eshu_dp_workflow_claim_quota_exceeded_total"); got != 1 {
		t.Fatalf("quota_exceeded_total = %d, want 1 (third claim throttled on scope B)", got)
	}
}
