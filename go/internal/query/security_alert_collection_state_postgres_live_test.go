// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8803 disposable PostgreSQL proof of the security-alert empty-answer collection-state reads on the bootstrapped schema: only enabled, non-deactivated security_alert instances contribute configuration; the latest work item per target is chosen by updated_at with the lowest work_item_id on a tie and ignores other collector kinds; a failed_terminal row with NULL last_failure_terminal (pre-migration-185) reads as failed_terminal with the flag false; a committed generation is active, superseded, or completed only; a target with no work items still returns a row; the 64-instance and 1024-target bounds fail with ErrSecurityAlertCollectionStateBound; EXPLAIN shows the lateral latest-item read on workflow_work_items_collector_scope_generation_updated_idx; isolated bootstrapped schema per test; wall time and plan timing are logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const securityAlertCollectionStateLiveSkip = "set ESHU_POSTGRES_DSN to run the #8803 security-alert collection-state live proofs"

// collectionStateLiveFixture seeds collector_instances, workflow_work_items,
// ingestion_scopes, and scope_generations in an isolated bootstrapped schema.
type collectionStateLiveFixture struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	asOf     time.Time
}

func newCollectionStateLiveFixture(t *testing.T, prefix string) *collectionStateLiveFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip(securityAlertCollectionStateLiveSkip)
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, prefix, func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	f := &collectionStateLiveFixture{t: t, ctx: ctx, database: database, asOf: time.Now().UTC().Truncate(time.Second)}
	f.exec(`INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ('run-8803', 'schedule', 'running', $1, $1)`, f.asOf)
	return f
}

func (f *collectionStateLiveFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.database.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatalf("exec %q: %v", strings.Fields(query)[0], err)
	}
}

func (f *collectionStateLiveFixture) instance(id, kind string, enabled, deactivated bool, configuration string) {
	f.t.Helper()
	var deactivatedAt any
	if deactivated {
		deactivatedAt = f.asOf.Add(-time.Hour)
	}
	f.exec(`INSERT INTO collector_instances (instance_id, collector_kind, mode, enabled, configuration,
    last_observed_at, deactivated_at, created_at, updated_at)
VALUES ($1, $2, 'continuous', $3, $4::jsonb, $5, $6, $5, $5)`, id, kind, enabled, configuration, f.asOf, deactivatedAt)
}

// item inserts one work item updated age before asOf. terminal is the
// last_failure_terminal column; nil stores NULL, as rows written before
// migration 185 do.
func (f *collectionStateLiveFixture) item(id, kind, scope, status, class string, terminal any, age time.Duration) {
	f.t.Helper()
	var lastClass any
	if class != "" {
		lastClass = class
	}
	updatedAt := f.asOf.Add(-age)
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, status, last_failure_class, last_failure_terminal, last_failure_message, created_at, updated_at)
VALUES ($1, 'run-8803', $2, 'sa-live', $3, $1, $4, $5, $6, 'provider text that must never be read', $7, $7)`,
		id, kind, scope, status, lastClass, terminal, updatedAt)
}

// generation inserts one scope generation with status, creating its scope.
func (f *collectionStateLiveFixture) generation(scope, status string) {
	f.t.Helper()
	f.exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
VALUES ($1, 'repository', 'github', $1, 'security_alert', $1, $2, $2, 'active')
ON CONFLICT (scope_id) DO NOTHING`, scope, f.asOf)
	f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'schedule', $3, $3, $4)`, scope+"-"+status, scope, f.asOf, status)
}

func (f *collectionStateLiveFixture) store() PostgresSecurityAlertCollectionStateStore {
	return NewPostgresSecurityAlertCollectionStateStoreWithReadStore(storagepostgres.SQLDB{DB: f.database})
}

func TestSecurityAlertCollectionStateSemanticsLive(t *testing.T) {
	started := time.Now()
	f := newCollectionStateLiveFixture(t, "sa_collection_state")
	f.instance("sa-live", "security_alert", true, false, `{"targets":[{"scope_id":"live"}]}`)
	f.instance("sa-disabled", "security_alert", false, false, `{"targets":[{"scope_id":"disabled"}]}`)
	f.instance("sa-deactivated", "security_alert", true, true, `{"targets":[{"scope_id":"deactivated"}]}`)
	f.instance("git-live", "git", true, false, `{"targets":[{"scope_id":"git"}]}`)

	configs, err := f.store().SecurityAlertCollectorConfigurations(f.ctx)
	if err != nil {
		t.Fatalf("SecurityAlertCollectorConfigurations() error = %v", err)
	}
	if len(configs) != 1 || !strings.Contains(configs[0], `"live"`) {
		t.Fatalf("configurations = %v, want only the enabled, non-deactivated security_alert instance", configs)
	}

	// Latest by updated_at: an older completion, a newer terminal failure, and
	// a still newer item of another collector kind that must be ignored.
	f.item("latest-old", "security_alert", "t-latest", "completed", "", nil, 2*time.Hour)
	f.item("latest-new", "security_alert", "t-latest", "failed_terminal", "auth_denied", true, time.Hour)
	f.item("latest-git", "git", "t-latest", "completed", "", nil, time.Minute)
	// Equal updated_at: the lowest work_item_id wins.
	f.item("tie-b", "security_alert", "t-tie", "failed_terminal", "not_found", true, time.Hour)
	f.item("tie-a", "security_alert", "t-tie", "completed", "", nil, time.Hour)
	// A pre-migration-185 terminal row: last_failure_terminal is NULL.
	f.item("f3-row", "security_alert", "t-f3", "failed_terminal", "auth_denied", nil, time.Hour)
	for _, status := range []string{"active", "superseded", "completed", "pending", "failed"} {
		f.generation("g-"+status, status)
	}

	states, err := f.store().SecurityAlertTargetStates(f.ctx, []string{
		"g-active", "g-completed", "g-failed", "g-pending", "g-superseded", "t-f3", "t-latest", "t-none", "t-tie",
	}, nil)
	if err != nil {
		t.Fatalf("SecurityAlertTargetStates() error = %v", err)
	}
	want := map[string]supplychain.SecurityAlertTargetState{
		"g-active":     {ScopeID: "g-active", CommittedGeneration: true},
		"g-completed":  {ScopeID: "g-completed", CommittedGeneration: true},
		"g-failed":     {ScopeID: "g-failed"},
		"g-pending":    {ScopeID: "g-pending"},
		"g-superseded": {ScopeID: "g-superseded", CommittedGeneration: true},
		"t-f3":         {ScopeID: "t-f3", LatestWorkItemStatus: "failed_terminal", LatestFailureClass: "auth_denied"},
		"t-latest":     {ScopeID: "t-latest", LatestWorkItemStatus: "failed_terminal", LatestFailureClass: "auth_denied", LatestFailureTerminal: true},
		"t-none":       {ScopeID: "t-none"},
		"t-tie":        {ScopeID: "t-tie", LatestWorkItemStatus: "completed"},
	}
	if len(states) != len(want) {
		t.Fatalf("states = %+v, want one row per requested scope (%d)", states, len(want))
	}
	for _, state := range states {
		if !reflect.DeepEqual(state, want[state.ScopeID]) {
			t.Fatalf("state %s = %+v, want %+v", state.ScopeID, state, want[state.ScopeID])
		}
	}
	t.Logf("semantics proof wall time %s", time.Since(started))
}

func TestSecurityAlertCollectionStateBoundsLive(t *testing.T) {
	f := newCollectionStateLiveFixture(t, "sa_collection_bounds")
	for i := 0; i < 64; i++ {
		f.instance(fmt.Sprintf("sa-%02d", i), "security_alert", true, false, `{"targets":[]}`)
	}
	configs, err := f.store().SecurityAlertCollectorConfigurations(f.ctx)
	if err != nil || len(configs) != 64 {
		t.Fatalf("64 instances: configs=%d err=%v, want 64 and no error", len(configs), err)
	}
	f.instance("sa-64", "security_alert", true, false, `{"targets":[]}`)
	if _, err := f.store().SecurityAlertCollectorConfigurations(f.ctx); !errors.Is(err, supplychain.ErrSecurityAlertCollectionStateBound) {
		t.Fatalf("65 instances: error = %v, want ErrSecurityAlertCollectionStateBound", err)
	}

	scopeIDs := make([]string, 1025)
	for i := range scopeIDs {
		scopeIDs[i] = fmt.Sprintf("bound-%04d", i)
	}
	states, err := f.store().SecurityAlertTargetStates(f.ctx, scopeIDs[:1024], nil)
	if err != nil || len(states) != 1024 {
		t.Fatalf("1024 targets: states=%d err=%v, want 1024 rows and no error", len(states), err)
	}
	if _, err := f.store().SecurityAlertTargetStates(f.ctx, scopeIDs, nil); !errors.Is(err, supplychain.ErrSecurityAlertCollectionStateBound) {
		t.Fatalf("1025 targets: error = %v, want ErrSecurityAlertCollectionStateBound", err)
	}
}

// TestSecurityAlertCollectionStatePlanLive seeds 50 targets with 2,000
// security_alert work items each plus unrelated git history, then asserts the
// lateral latest-item read uses the collector/scope index and logs the plan's
// execution time and shared buffers.
func TestSecurityAlertCollectionStatePlanLive(t *testing.T) {
	f := newCollectionStateLiveFixture(t, "sa_collection_plan")
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, status, created_at, updated_at)
SELECT 'wi-' || k || '-' || t || '-' || g, 'run-8803', k, 'sa-live', 'target-' || t, md5(k || t || g),
       CASE WHEN g % 10 = 0 THEN 'failed_retryable' ELSE 'completed' END,
       $1::timestamptz - (g || ' minutes')::interval, $1::timestamptz - (g || ' minutes')::interval
FROM unnest(ARRAY['security_alert', 'git']) AS k, generate_series(1, 50) AS t, generate_series(1, 2000) AS g`, f.asOf)
	f.exec(`ANALYZE workflow_work_items`)
	scopeIDs := make([]string, 50)
	for i := range scopeIDs {
		scopeIDs[i] = fmt.Sprintf("target-%d", i+1)
	}
	// Freshly inserted rows have no visibility-map bits, so the first plan
	// reads every history row from the heap; after VACUUM the same read is
	// index-only. Both must use the collector/scope index.
	cold := explainSecurityAlertTargetStates(f, scopeIDs)
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) before VACUUM, 50 targets x 2,000 security_alert items plus 100,000 git items:\n%s", cold)
	f.exec(`VACUUM (ANALYZE) workflow_work_items`)
	warm := explainSecurityAlertTargetStates(f, scopeIDs)
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) after VACUUM (ANALYZE):\n%s", warm)
}

func explainSecurityAlertTargetStates(f *collectionStateLiveFixture, scopeIDs []string) string {
	f.t.Helper()
	rows, err := f.database.QueryContext(f.ctx, "EXPLAIN (ANALYZE, BUFFERS) "+securityAlertTargetStatesQuery, array.Of(scopeIDs), array.Of([]string(nil)))
	if err != nil {
		f.t.Fatalf("EXPLAIN target states: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			f.t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		f.t.Fatalf("read plan: %v", err)
	}
	text := plan.String()
	if !strings.Contains(text, "workflow_work_items_collector_scope_generation_updated_idx") {
		f.t.Fatalf("latest-item read does not use workflow_work_items_collector_scope_generation_updated_idx:\n%s", text)
	}
	return text
}
