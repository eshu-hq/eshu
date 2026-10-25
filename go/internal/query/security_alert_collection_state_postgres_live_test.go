// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8803 disposable PostgreSQL proof of the security-alert empty-answer collection-state reads on the bootstrapped schema: only enabled, non-deactivated security_alert instances contribute configuration; the latest work item per target is chosen by updated_at with the lowest work_item_id on a tie and ignores other collector kinds; a failed_terminal row with NULL last_failure_terminal (pre-migration-185) reads as failed_terminal with the flag false; a committed generation is active, superseded, or completed only; a target with no work items still returns a row; the 64-instance and 1024-target bounds fail with ErrSecurityAlertCollectionStateBound; EXPLAIN shows the lateral latest-item read as an index-only probe of workflow_work_items_security_alert_scope_updated_idx (#8840) with no sort over a 20,000-item history, and it returns the same rows as the pre-#8840 plan forced by dropping the index in a rolled-back transaction, including updated_at ties; isolated bootstrapped schema per test; wall time and plan timing are logged, not gated
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
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
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
// security_alert work items each, one long-history target with 20,000, ties on
// updated_at, and unrelated git history. It asserts the lateral latest-item
// read is an index probe on workflow_work_items_security_alert_scope_updated_idx
// (#8840) with no sort over the target's history, and that it returns the same
// rows as the pre-#8840 plan, which is forced inside a rolled-back transaction
// that drops the index. Plan timing and buffers are logged, not gated.
func TestSecurityAlertCollectionStatePlanLive(t *testing.T) {
	f := newCollectionStateLiveFixture(t, "sa_collection_plan")
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, status, created_at, updated_at)
SELECT 'wi-' || k || '-' || t || '-' || g, 'run-8803', k, 'sa-live', 'target-' || t, md5(k || t || g),
       CASE WHEN g % 10 = 0 THEN 'failed_retryable' ELSE 'completed' END,
       $1::timestamptz - (g || ' minutes')::interval, $1::timestamptz - (g || ' minutes')::interval
FROM unnest(ARRAY['security_alert', 'git']) AS k, generate_series(1, 50) AS t, generate_series(1, 2000) AS g`, f.asOf)
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, last_failure_class, last_failure_terminal, status, created_at, updated_at)
SELECT 'wi-long-' || g, 'run-8803', 'security_alert', 'sa-live', 'target-long', md5('long' || g),
       CASE WHEN g = 1 THEN 'not_found' END, g = 1,
       CASE WHEN g = 1 THEN 'failed_terminal' ELSE 'completed' END,
       $1::timestamptz - (g || ' seconds')::interval, $1::timestamptz - (g || ' seconds')::interval
FROM generate_series(1, 20000) AS g`, f.asOf)
	// Every odd target gets a failed_terminal row that ties its newest item on
	// updated_at and sorts first by work_item_id, so the tiebreak decides it.
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, last_failure_class, last_failure_terminal, status, created_at, updated_at)
SELECT 'wi-security_alert-' || t || '-0', 'run-8803', 'security_alert', 'sa-live', 'target-' || t, md5('tie' || t),
       'auth_denied', TRUE, 'failed_terminal', $1::timestamptz - interval '1 minute', $1::timestamptz - interval '1 minute'
FROM generate_series(1, 50, 2) AS t`, f.asOf)
	f.exec(`ANALYZE workflow_work_items`)
	scopeIDs := []string{"target-long", "target-missing"}
	for i := 1; i <= 50; i++ {
		scopeIDs = append(scopeIDs, fmt.Sprintf("target-%d", i))
	}
	// Freshly inserted rows have no visibility-map bits, so the first plan
	// fetches the one newest row per target from the heap; after VACUUM it is
	// index-only. Both must probe the #8840 index without a history sort.
	cold := explainSecurityAlertTargetStates(f, scopeIDs)
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) before VACUUM, 50 targets x 2,000 plus one 20,000-item target and 100,000 git items:\n%s", cold)
	f.exec(`VACUUM (ANALYZE) workflow_work_items`)
	warm := explainSecurityAlertTargetStates(f, scopeIDs)
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) after VACUUM (ANALYZE):\n%s", warm)

	withIndex, err := f.store().SecurityAlertTargetStates(f.ctx, scopeIDs, nil)
	if err != nil {
		t.Fatalf("SecurityAlertTargetStates() error = %v", err)
	}
	withoutIndex := securityAlertTargetStatesWithoutIndex(f, scopeIDs)
	if len(withIndex) != len(scopeIDs) || len(withoutIndex) != len(withIndex) {
		t.Fatalf("rows with index = %d, without = %d, want %d each", len(withIndex), len(withoutIndex), len(scopeIDs))
	}
	for i := range withIndex {
		if !reflect.DeepEqual(withIndex[i], withoutIndex[i]) {
			t.Fatalf("row %d with index = %+v, pre-#8840 plan = %+v", i, withIndex[i], withoutIndex[i])
		}
	}
	byScope := map[string]supplychain.SecurityAlertTargetState{}
	for _, state := range withIndex {
		byScope[state.ScopeID] = state
	}
	for scope, want := range map[string]supplychain.SecurityAlertTargetState{
		"target-long":    {ScopeID: "target-long", LatestWorkItemStatus: "failed_terminal", LatestFailureClass: "not_found", LatestFailureTerminal: true},
		"target-missing": {ScopeID: "target-missing"},
		"target-1":       {ScopeID: "target-1", LatestWorkItemStatus: "failed_terminal", LatestFailureClass: "auth_denied", LatestFailureTerminal: true},
		"target-2":       {ScopeID: "target-2", LatestWorkItemStatus: "completed"},
	} {
		if !reflect.DeepEqual(byScope[scope], want) {
			t.Fatalf("state %s = %+v, want %+v", scope, byScope[scope], want)
		}
	}
}

// securityAlertTargetStatesWithoutIndex runs the production read with the
// #8840 index dropped inside a transaction that is rolled back, so the planner
// takes the pre-#8840 plan over the same rows.
func securityAlertTargetStatesWithoutIndex(f *collectionStateLiveFixture, scopeIDs []string) []supplychain.SecurityAlertTargetState {
	f.t.Helper()
	tx, err := f.database.BeginTx(f.ctx, nil)
	if err != nil {
		f.t.Fatalf("begin differential transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(f.ctx, "DROP INDEX "+securityAlertLatestWorkItemIndex); err != nil {
		f.t.Fatalf("drop index inside differential transaction: %v", err)
	}
	var plan strings.Builder
	planRows, err := tx.QueryContext(f.ctx, "EXPLAIN "+securityAlertTargetStatesQuery, array.Of(scopeIDs), array.Of([]string(nil)), shard.RetirementFreshnessHint)
	if err != nil {
		f.t.Fatalf("EXPLAIN pre-#8840 plan: %v", err)
	}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			f.t.Fatalf("scan pre-#8840 plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	_ = planRows.Close()
	if strings.Contains(plan.String(), securityAlertLatestWorkItemIndex) {
		f.t.Fatalf("differential plan still uses the dropped index:\n%s", plan.String())
	}
	states, err := NewPostgresSecurityAlertCollectionStateStoreWithReadStore(storagepostgres.SQLTx{Tx: tx}).SecurityAlertTargetStates(f.ctx, scopeIDs, nil)
	if err != nil {
		f.t.Fatalf("SecurityAlertTargetStates() on the pre-#8840 plan: %v", err)
	}
	return states
}

// securityAlertLatestWorkItemIndex is the migration 233 partial index the lateral
// latest-item read must probe.
const securityAlertLatestWorkItemIndex = "workflow_work_items_security_alert_scope_updated_idx"

func explainSecurityAlertTargetStates(f *collectionStateLiveFixture, scopeIDs []string) string {
	f.t.Helper()
	rows, err := f.database.QueryContext(f.ctx, "EXPLAIN (ANALYZE, BUFFERS) "+securityAlertTargetStatesQuery, array.Of(scopeIDs), array.Of([]string(nil)),
		shard.RetirementFreshnessHint)
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
	if !strings.Contains(text, "Index Only Scan using "+securityAlertLatestWorkItemIndex) {
		f.t.Fatalf("latest-item read is not an index-only probe of %s:\n%s", securityAlertLatestWorkItemIndex, text)
	}
	if strings.Contains(text, "Sort Key: l.updated_at") {
		f.t.Fatalf("latest-item read still sorts the target's history:\n%s", text)
	}
	return text
}
