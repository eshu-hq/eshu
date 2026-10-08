// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestRelationshipReopenSkipsSupersededGenerations is the #7637 RED test. The
// deferred maintenance pass's deployment_mapping and code_import_repo_edge
// reopen listings have no replay floor: every succeeded row in the store is
// listed and reopened, including rows on generations the scope has long since
// superseded. The correlation listing
// (listSucceededReducerWorkItemsByDomainQuery) bounds itself to each scope's
// active generation or newer with a failed-generation exclusion; these two
// listings must carry the same bound.
//
// Fixture shapes, identical for both relationship domains:
//
//   - scope-a re-ingested: gen-old's succeeded rows are dead history under the
//     now-active gen-new. They must stay 'succeeded'.
//   - scope-b never activated: active_generation_id is NULL and the latest
//     generation is live. Its rows must still reopen — this is the shape the
//     replay exists for, not superseded history.
//   - scope-c failed: the latest generation failed and the scope pointer was
//     nulled (the shape failProjectorWorkQuery writes). Its rows must stay
//     'succeeded': nothing the failed generation re-decides is ever read.
//
// On current main the stale and failed rows reopen (RED); with the floor they
// do not (GREEN). The active and never-activated rows reopen in both arms.
func TestRelationshipReopenSkipsSupersededGenerations(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-old", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
		{scopeID: "git:scope-c", genID: "gen-c-old", repoID: "repo-c", repoName: "gamma-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, nil, base)

	// scope-a re-ingested: gen-new is strictly newer and is now the active
	// generation, so gen-old's succeeded rows are dead history.
	seedSupersedingActiveGeneration(t, ctx, db, "git:scope-a", "gen-new", base.Add(time.Hour))

	// scope-c failed: the latest generation failed and the scope pointer is
	// NULL, the shape failProjectorWorkQuery writes.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO scope_generations (generation_id, scope_id, ingested_at, status)
		 VALUES ('gen-c-failed', 'git:scope-c', $1, 'failed')`,
		base.Add(time.Hour)); err != nil {
		t.Fatalf("seed failed generation: %v", err)
	}

	domains := []string{"deployment_mapping", "code_import_repo_edge"}
	for _, domain := range domains {
		seedSucceededReopenWorkItem(t, ctx, db, "work-stale-"+domain, "git:scope-a", "gen-old", domain, base)
		seedSucceededReopenWorkItem(t, ctx, db, "work-active-"+domain, "git:scope-a", "gen-new", domain, base)
		seedSucceededReopenWorkItem(t, ctx, db, "work-unactivated-"+domain, "git:scope-b", "gen-b", domain, base)
		seedSucceededReopenWorkItem(t, ctx, db, "work-failed-"+domain, "git:scope-c", "gen-c-failed", domain, base)
	}

	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return base }
	if err := store.ReopenDeploymentMappingWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenDeploymentMappingWorkItems() error = %v", err)
	}
	if err := store.ReopenCodeImportRepoEdgeWorkItems(ctx, nil, nil); err != nil {
		t.Fatalf("ReopenCodeImportRepoEdgeWorkItems() error = %v", err)
	}

	for _, domain := range domains {
		if got, want := workItemStatus(t, ctx, db, "work-stale-"+domain), "succeeded"; got != want {
			t.Errorf("domain %s superseded work item status = %q, want %q (the floor must not reopen dead history)",
				domain, got, want)
		}
		if got, want := workItemStatus(t, ctx, db, "work-active-"+domain), "pending"; got != want {
			t.Errorf("domain %s active work item status = %q, want %q (the floor must keep the live generation)",
				domain, got, want)
		}
		if got, want := workItemStatus(t, ctx, db, "work-unactivated-"+domain), "pending"; got != want {
			t.Errorf("domain %s never-activated work item status = %q, want %q (no active generation is not superseded)",
				domain, got, want)
		}
		if got, want := workItemStatus(t, ctx, db, "work-failed-"+domain), "succeeded"; got != want {
			t.Errorf("domain %s failed-generation work item status = %q, want %q (a failed generation's re-decision is never read)",
				domain, got, want)
		}
	}
}

// TestRelationshipReopenMatchesCorrelationReplayFloor is the #7637 live
// differential: the relationship listings must reopen exactly the partitions
// the SHIPPED correlation listing reopens for the same fixture, proving the
// floor is derived from that query rather than hand-copied. A hand copy that
// drifted (wrong fallback, missing exclusion) would show up here as a
// partition-set mismatch against the shipped text.
func TestRelationshipReopenMatchesCorrelationReplayFloor(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-old", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
		{scopeID: "git:scope-c", genID: "gen-c-old", repoID: "repo-c", repoName: "gamma-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, nil, base)
	seedSupersedingActiveGeneration(t, ctx, db, "git:scope-a", "gen-new", base.Add(time.Hour))
	if _, err := db.ExecContext(ctx,
		`INSERT INTO scope_generations (generation_id, scope_id, ingested_at, status)
		 VALUES ('gen-c-failed', 'git:scope-c', $1, 'failed')`,
		base.Add(time.Hour)); err != nil {
		t.Fatalf("seed failed generation: %v", err)
	}

	// Every generation carries one succeeded row per domain, so the listings
	// choose purely on the floor.
	generations := map[string][]string{
		"git:scope-a": {"gen-old", "gen-new"},
		"git:scope-b": {"gen-b"},
		"git:scope-c": {"gen-c-old", "gen-c-failed"},
	}
	correlationDomain := string(reducer.DomainSupplyChainImpact)
	for scopeID, genIDs := range generations {
		for _, genID := range genIDs {
			seedSucceededReopenWorkItem(t, ctx, db,
				"work-dm-"+scopeID+"-"+genID, scopeID, genID, "deployment_mapping", base)
			seedSucceededReopenWorkItem(t, ctx, db,
				"work-ci-"+scopeID+"-"+genID, scopeID, genID, "code_import_repo_edge", base)
			seedSucceededReopenWorkItem(t, ctx, db,
				"work-corr-"+scopeID+"-"+genID, scopeID, genID, correlationDomain, base)
		}
	}

	adapter := SQLDB{DB: db}
	deploymentItems, err := listSucceededDeploymentMappingWorkItems(ctx, adapter)
	if err != nil {
		t.Fatalf("listSucceededDeploymentMappingWorkItems() error = %v", err)
	}
	codeImportItems, err := listSucceededCodeImportRepoEdgeWorkItems(ctx, adapter)
	if err != nil {
		t.Fatalf("listSucceededCodeImportRepoEdgeWorkItems() error = %v", err)
	}
	correlationIDs, err := listSucceededReducerWorkItemIDsForDomain(ctx, adapter, correlationDomain)
	if err != nil {
		t.Fatalf("listSucceededReducerWorkItemIDsForDomain() error = %v", err)
	}

	deploymentPartitions := reopenRefPartitionSet(deploymentItems)
	codeImportPartitions := reopenRefPartitionSet(codeImportItems)
	correlationPartitions := workItemIDPartitionSet(t, ctx, db, correlationIDs)

	if !partitionSetsEqual(deploymentPartitions, correlationPartitions) {
		t.Errorf("deployment_mapping partitions = %v, want correlation partitions %v",
			sortedPartitionKeys(deploymentPartitions), sortedPartitionKeys(correlationPartitions))
	}
	if !partitionSetsEqual(codeImportPartitions, correlationPartitions) {
		t.Errorf("code_import_repo_edge partitions = %v, want correlation partitions %v",
			sortedPartitionKeys(codeImportPartitions), sortedPartitionKeys(correlationPartitions))
	}
}

// reopenRefPartitionSet keys reopenWorkItemRef rows by partition.
func reopenRefPartitionSet(items []reopenWorkItemRef) map[scopeGenerationPartition]struct{} {
	set := make(map[scopeGenerationPartition]struct{}, len(items))
	for _, item := range items {
		set[item.Partition] = struct{}{}
	}
	return set
}

// workItemIDPartitionSet resolves work item IDs back to their partitions.
func workItemIDPartitionSet(
	t *testing.T, ctx context.Context, db *sql.DB, workItemIDs []string,
) map[scopeGenerationPartition]struct{} {
	t.Helper()
	set := make(map[scopeGenerationPartition]struct{}, len(workItemIDs))
	for _, workItemID := range workItemIDs {
		var partition scopeGenerationPartition
		if err := db.QueryRowContext(ctx,
			"SELECT scope_id, generation_id FROM fact_work_items WHERE work_item_id = $1",
			workItemID).Scan(&partition.ScopeID, &partition.GenerationID); err != nil {
			t.Fatalf("resolve partition for work item %q: %v", workItemID, err)
		}
		set[partition] = struct{}{}
	}
	return set
}

// partitionSetsEqual reports whether two partition sets hold the same members.
func partitionSetsEqual(a, b map[scopeGenerationPartition]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for partition := range a {
		if _, ok := b[partition]; !ok {
			return false
		}
	}
	return true
}

// sortedPartitionKeys renders a partition set deterministically for errors.
func sortedPartitionKeys(set map[scopeGenerationPartition]struct{}) []scopeGenerationPartition {
	return sortedPartitions(set)
}
