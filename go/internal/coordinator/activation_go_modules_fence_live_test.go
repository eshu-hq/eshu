// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10055 real coordinator Service and PostgreSQL proof that a fence lock timeout leaves no run row, so the next reconcile re-reads the page and retries instead of waiting a rotation
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/gomodules"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// seedSkewedGoRepository writes one Go repository at activeCommit whose go-reachability
// scope holds evidence of another commit it left three hours before now.
func seedSkewedGoRepository(t *testing.T, database *sql.DB, now time.Time) (scopeID, generationID string) {
	t.Helper()
	const repositoryID = "repository:r_00000001"
	repositoryScope := "git-repository-scope:" + repositoryID
	scopeID = "go-reachability://" + repositoryID
	generationID = "reach-generation"
	evidenceCommit := strings.Repeat("e", 40)
	left := now.Add(-3 * time.Hour)
	for _, step := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		    observed_at, ingested_at, status, payload)
		  VALUES ($1, 'repository', 'git', $2, 'git', 'p', now(), now(), 'active',
		    jsonb_build_object('repo_id', $2::text, 'remote_url', 'https://github.com/eshu-hq/repo-1'))`, []any{repositoryScope, repositoryID}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, source_commit_sha)
		  VALUES ('repo-generation', $1, 'snapshot', now(), now(), 'active', $2)`, []any{repositoryScope, testGoCommit}},
		{`UPDATE ingestion_scopes SET active_generation_id = 'repo-generation' WHERE scope_id = $1`, []any{repositoryScope}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, source_commit_sha, superseded_at)
		  VALUES ('repo-generation-old', $1, 'snapshot', $2, $2, 'superseded', $3, $2)`, []any{repositoryScope, left, evidenceCommit}},
		{`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
		  VALUES ($1, 'go.mod', 'module example.com/m', 'h', 1, now())`, []any{repositoryID}},
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		    observed_at, ingested_at, status, payload)
		  VALUES ($1, 'scanner_worker', 'scanner_worker', $1, 'scanner_worker', 'p', now(), now(), 'active', '{}'::jsonb)`, []any{scopeID}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, source_commit_sha, activated_at)
		  VALUES ($1, $2, 'snapshot', now(), now(), 'active', $3, now())`, []any{generationID, scopeID, evidenceCommit}},
		{`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, []any{generationID, scopeID}},
		{`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
		    observed_at, ingested_at, payload)
		  VALUES ('reach-fact', $1, $2, 'vulnerability.go_module_evidence', 'reach-fact', 'scanner_worker', 'reach-fact', now(), now(), '{}'::jsonb)`, []any{scopeID, generationID}},
	} {
		if _, err := database.ExecContext(t.Context(), step.query, step.args...); err != nil {
			t.Fatalf("seed %q: %v", step.query, err)
		}
	}
	return scopeID, generationID
}

// TestGoModulesFenceLockTimeoutLeavesNoRunAndRetriesLive is Amendment 1 case (h): a
// held scope row fails the pass by lock timeout with no run row, because a run would stop
// the bucket at the planned probe. Once the row is free the next tick fences and plans.
func TestGoModulesFenceLockTimeoutLeavesNoRunAndRetriesLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set")
	}
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db})
		})
	now := time.Now().UTC()
	scopeID, generationID := seedSkewedGoRepository(t, database, now)

	control := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))
	if err := control.ReconcileCollectorInstances(t.Context(), now, []workflow.DesiredCollectorInstance{{
		InstanceID: instance.InstanceID, CollectorKind: instance.CollectorKind, Mode: instance.Mode,
		Enabled: true, ClaimsEnabled: true, Configuration: instance.Configuration,
	}}); err != nil {
		t.Fatal(err)
	}
	store := gomodules.NewStore(postgres.SQLDB{DB: database}).WithHolderReopener(postgres.ReopenSupplyChainImpactHolderInTransaction)
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive, ClaimsEnabled: true, ReconcileInterval: time.Hour,
			ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, `{"mode":"broad"}`),
		},
		Store:                     control,
		ComponentExtensionPlanner: extension.WorkPlanner{},
		GoModuleRepositoryReader:  store,
		GoEvidenceFencer:          store,
		GovernanceAudit:           &fakeGovernanceAuditAppender{},
		Clock:                     func() time.Time { return now },
	}
	countRuns := func() int {
		t.Helper()
		var runs int
		if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM workflow_runs`).Scan(&runs); err != nil {
			t.Fatal(err)
		}
		return runs
	}
	readPointer := func() sql.NullString {
		t.Helper()
		var pointer sql.NullString
		if err := database.QueryRowContext(t.Context(),
			`SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1`, scopeID).Scan(&pointer); err != nil {
			t.Fatal(err)
		}
		return pointer
	}

	holder, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	var held string
	if err := holder.QueryRowContext(t.Context(),
		`SELECT scope_id FROM ingestion_scopes WHERE scope_id = $1 FOR NO KEY UPDATE`, scopeID).Scan(&held); err != nil {
		t.Fatalf("hold the scope row: %v", err)
	}

	err = service.scheduleComponentExtensionWork(t.Context(), now, []workflow.CollectorInstance{instance})
	if err == nil || !strings.Contains(err.Error(), "fence stale go evidence") {
		t.Fatalf("schedule error = %v, want the fence's lock timeout", err)
	}
	if got := countRuns(); got != 0 {
		t.Fatalf("runs after a failed fence = %d, want 0", got)
	}
	if pointer := readPointer(); pointer.String != generationID {
		t.Fatalf("pointer after a failed fence = %+v, want %q", pointer, generationID)
	}

	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := service.scheduleComponentExtensionWork(t.Context(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("schedule after the row is free: %v", err)
	}
	if got := countRuns(); got != 1 {
		t.Fatalf("runs after the retry = %d, want 1", got)
	}
	if pointer := readPointer(); pointer.Valid {
		t.Fatalf("pointer after the retry = %+v, want NULL", pointer)
	}
}
