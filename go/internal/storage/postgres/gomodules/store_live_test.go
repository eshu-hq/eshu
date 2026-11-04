// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10027 real PostgreSQL proof of the go.mod rotation page read: every repository visited once per rotation, Go repositories only, active-generation commit, no read after the bucket's run exists, and the real admission path keeping a repeat pass idempotent
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/reachability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func openStoreProof(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set")
	}
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db})
		})
	return database
}

// seedRepository writes one repository scope with an active generation, shaped
// as the git collector writes it. goPath names a stored go.mod, or none when empty.
func seedRepository(t *testing.T, database *sql.DB, ordinal int, goPath string) string {
	t.Helper()
	repositoryID := fmt.Sprintf("repository:r_%08x", ordinal)
	scopeID := "git-repository-scope:" + repositoryID
	generationID := fmt.Sprintf("gen-%d", ordinal)
	commit := fmt.Sprintf("%040x", ordinal)
	ctx := t.Context()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
    observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $2, 'git', 'p', now(), now(), 'active',
    jsonb_build_object('repo_id', $2::text, 'remote_url', 'https://github.com/eshu-hq/repo-' || $3::text))`,
		scopeID, repositoryID, fmt.Sprint(ordinal)); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, source_commit_sha)
VALUES ($1, $2, 'snapshot', now(), now(), 'active', $3)`, generationID, scopeID, commit); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		generationID, scopeID); err != nil {
		t.Fatalf("activate generation: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ($1, 'main.txt', 'x', 'h', 1, now())`, repositoryID); err != nil {
		t.Fatalf("seed content: %v", err)
	}
	if goPath != "" {
		if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ($1, $2, 'module example.com/m', 'h', 1, now())`, repositoryID, goPath); err != nil {
			t.Fatalf("seed go.mod: %v", err)
		}
	}
	return repositoryID
}

// TestListGoModuleRepositoriesRotatesEveryRepositoryLive proves one rotation
// visits every repository exactly once, returns only Go repositories (root and
// nested go.mod), carries the active generation's commit and the stored remote,
// and never treats a look-alike file name as a go.mod.
func TestListGoModuleRepositoriesRotatesEveryRepositoryLive(t *testing.T) {
	database := openStoreProof(t)
	const pageSize, fleet = 4, 10
	wantGo := map[string]string{}
	for ordinal := range fleet {
		goPath := ""
		switch ordinal % 3 {
		case 0:
			goPath = "go.mod"
		case 1:
			goPath = "services/api/go.mod"
		}
		id := seedRepository(t, database, ordinal, goPath)
		if goPath != "" {
			wantGo[id] = fmt.Sprintf("%040x", ordinal)
		}
	}
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('repository:r_00000002', 'notgo.mod', 'x', 'h', 1, now())`); err != nil {
		t.Fatal(err)
	}

	store := NewStore(postgres.SQLDB{DB: database})
	seen := map[string]int{}
	for bucket := range int64(3) {
		page, err := store.ListGoModuleRepositories(t.Context(), scannercomponent.GoModuleRepositoryFilter{
			RotationBucket: bucket, PageSize: pageSize,
		})
		if err != nil {
			t.Fatalf("bucket %d: %v", bucket, err)
		}
		if page.TotalCandidates != fleet || page.SlotCount != 3 || page.Slot != int(bucket) {
			t.Fatalf("bucket %d: page = %+v", bucket, page)
		}
		if page.Candidates > pageSize {
			t.Fatalf("bucket %d: candidates = %d, over the page size", bucket, page.Candidates)
		}
		for _, repository := range page.Repositories {
			seen[repository.RepositoryID]++
			if want, ok := wantGo[repository.RepositoryID]; !ok || repository.SourceCommitSHA != want {
				t.Fatalf("repository %+v, want a Go repository at commit %q", repository, want)
			}
			if !scannercomponent.ValidCloneURL(repository.RemoteURL) {
				t.Fatalf("remote %q is not plannable", repository.RemoteURL)
			}
		}
	}
	if len(seen) != len(wantGo) {
		t.Fatalf("rotation saw %d Go repositories, want %d", len(seen), len(wantGo))
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("repository %q visited %d times in one rotation", id, count)
		}
	}
	wrapped, err := store.ListGoModuleRepositories(t.Context(), scannercomponent.GoModuleRepositoryFilter{RotationBucket: 3, PageSize: pageSize})
	if err != nil || wrapped.Slot != 0 {
		t.Fatalf("bucket 3 wrap: page = %+v err = %v, want slot 0", wrapped, err)
	}
}

// TestListGoModuleRepositoriesSkipsInactiveScopesLive proves a scope without an
// active generation, a non-repository scope, and a non-git scope are never
// candidates, and an empty fleet reads as one empty slot.
func TestListGoModuleRepositoriesSkipsInactiveScopesLive(t *testing.T) {
	database := openStoreProof(t)
	store := NewStore(postgres.SQLDB{DB: database})

	empty, err := store.ListGoModuleRepositories(t.Context(), scannercomponent.GoModuleRepositoryFilter{PageSize: 4})
	if err != nil || empty.Candidates != 0 || empty.SlotCount != 1 {
		t.Fatalf("empty fleet: page = %+v err = %v", empty, err)
	}

	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
		 VALUES ('s-no-generation', 'repository', 'git', 'repository:r_0000aaaa', 'git', 'p', now(), now(), 'active')`,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
		 VALUES ('s-ref', 'repository_ref', 'git', 'repository:r_0000bbbb', 'git', 'p', now(), now(), 'active')`,
		`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
		 VALUES ('repository:r_0000aaaa', 'go.mod', 'module x', 'h', 1, now())`,
	} {
		if _, err := database.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListGoModuleRepositories(t.Context(), scannercomponent.GoModuleRepositoryFilter{PageSize: 4})
	if err != nil || page.Candidates != 0 {
		t.Fatalf("page = %+v err = %v, want no candidates", page, err)
	}
}

// TestGoModulePageAdmissionIsIdempotentLive drives the read, the planner, and
// the real guarded admission: a page lands once, and once the bucket's run exists
// the read stops, even after a commit moves, so the stored target cannot change
// under an open claim.
func TestGoModulePageAdmissionIsIdempotentLive(t *testing.T) {
	database := openStoreProof(t)
	id := seedRepository(t, database, 1, "go.mod")
	seedRepository(t, database, 2, "")

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID: "scanner-go-reachability", CollectorKind: scope.CollectorScannerWorker,
		Mode: workflow.CollectorModeScheduled, Enabled: true, ClaimsEnabled: true, Configuration: `{}`,
		CreatedAt: now, UpdatedAt: now, LastObservedAt: now,
	}
	control := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
	if err := control.ReconcileCollectorInstances(t.Context(), now, []workflow.DesiredCollectorInstance{{
		InstanceID: instance.InstanceID, CollectorKind: instance.CollectorKind, Mode: instance.Mode,
		Enabled: true, ClaimsEnabled: true, Configuration: instance.Configuration,
	}}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(postgres.SQLDB{DB: database})
	const planKey = "go-modules-scheduled-20261104T120000Z"
	filter := scannercomponent.GoModuleRepositoryFilter{RunID: reachability.RunID(instance, planKey), PageSize: scannercomponent.GoModulePageSize}

	page, err := store.ListGoModuleRepositories(t.Context(), filter)
	if err != nil || page.AlreadyPlanned || len(page.Repositories) != 1 {
		t.Fatalf("first read: page = %+v err = %v", page, err)
	}
	run, items, _, err := reachability.Plan(reachability.PlanRequest{
		Instance: instance, ObservedAt: now, PlanKey: planKey, Repositories: page.Repositories,
	})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := control.CreateRunWithWorkItemsIfNoOpenTargets(t.Context(), run, items)
	if err != nil || admission.InsertedWorkItems != 1 {
		t.Fatalf("admission = %+v err = %v, want one item", admission, err)
	}

	if _, err := database.ExecContext(t.Context(),
		`UPDATE scope_generations SET source_commit_sha = $1 WHERE scope_id = $2`,
		fmt.Sprintf("%040x", 999), "git-repository-scope:"+id); err != nil {
		t.Fatal(err)
	}
	repeat, err := store.ListGoModuleRepositories(t.Context(), filter)
	if err != nil || !repeat.AlreadyPlanned || repeat.Candidates != 0 {
		t.Fatalf("repeat read: page = %+v err = %v, want AlreadyPlanned", repeat, err)
	}
	raw, err := control.GetRunRequestedScopeSet(t.Context(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := scannercomponent.ResolveGoReachabilityTarget(raw, items[0].ScopeID)
	if err != nil {
		t.Fatalf("resolve target after the JSONB round trip: %v", err)
	}
	if target.SourceCommitSHA != fmt.Sprintf("%040x", 1) {
		t.Fatalf("stored commit = %q, want the planned one", target.SourceCommitSHA)
	}
}
