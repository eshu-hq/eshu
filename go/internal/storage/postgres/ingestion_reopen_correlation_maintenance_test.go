// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestRunDeferredRelationshipMaintenanceReopensCrossScopeCorrelationDomains is
// the P1 regression proof for the codex finding on PR #5846: the correlation
// domains were replayed ONLY by eshu-bootstrap-index's maintenance phase, never
// by the ingester. The live ingester runs
// RunDeferredRelationshipMaintenanceAfterShardDrain ->
// RunDeferredRelationshipMaintenance, which before this change replayed only
// deployment_mapping and code_import_repo_edge. A container_image_identity,
// ci_cd_run_correlation, or supply_chain_impact work item that lost the
// cross-scope activation race therefore kept its empty-join decision forever in
// normal ingestion, while the golden-corpus gate — which drives
// eshu-bootstrap-index for its maintenance passes — went green.
func TestRunDeferredRelationshipMaintenanceReopensCrossScopeCorrelationDomains(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-a", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, map[string]string{
		"repo-a": "beta-service",
	}, base)

	// One succeeded work item per cross-scope correlation domain, each standing
	// for a decision written before the producer scope's generation activated.
	// scope-a's active_generation_id is NULL here (seedMemoProofScopesAndFacts
	// leaves it unset), which is the activation-race shape itself: the reopen
	// must not treat "no active generation yet" as "superseded".
	for _, domain := range CrossScopeCorrelationReopenDomains() {
		seedSucceededReopenWorkItem(t, ctx, db, "work-"+domain+"-a", "git:scope-a", "gen-a", domain, base)
	}

	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return base }

	if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v", err)
	}

	// THE ASSERTION: the ingester's own maintenance entrypoint must replay every
	// cross-scope correlation domain, not just the two relationship domains.
	for _, domain := range CrossScopeCorrelationReopenDomains() {
		if got, want := workItemStatus(t, ctx, db, "work-"+domain+"-a"), "pending"; got != want {
			t.Fatalf(
				"domain %s work item status after ingester maintenance = %q, want %q "+
					"(the ingester path must replay the correlation domains, not only eshu-bootstrap-index)",
				domain, got, want,
			)
		}
	}
}

// TestRunDeferredRelationshipMaintenanceSkipsSupersededCorrelationWorkItems is
// the bound proof for the same change. Succeeded reducer rows are never
// superseded (supersedeInactiveReducerGenerationsCTE terminalizes only
// pending/retrying/failed/dead_letter), so one row per (scope, generation,
// domain) accumulates for the life of the store. Unlike eshu-bootstrap-index,
// which runs once, RunDeferredRelationshipMaintenance runs on EVERY shard
// drain, so an unbounded corpus-wide replay would resurrect the whole
// ingestion history into 'pending' on every drain and grow linearly with
// generation count.
//
// A row below the scope's replay floor is provably not worth replaying: the
// fact-backed correlation read surfaces join
// scope.active_generation_id = fact.generation_id
// (facts_active_container_image_identity.go, facts_active_cicd_run_correlation.go,
// facts_active_supply_chain_impact.go), so a stale generation's re-decision is
// written to rows no query ever reads; and the two graph-edge writers
// (deployable_unit_correlation, kubernetes_correlation_materialization) would
// spend graph writes anchoring edges to a generation the read surfaces no longer
// resolve. A scope that has not activated any generation yet still reopens — its
// latest generation IS the floor.
func TestRunDeferredRelationshipMaintenanceSkipsSupersededCorrelationWorkItems(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-old", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, map[string]string{
		"repo-a": "beta-service",
	}, base)

	// scope-a re-ingested: gen-new is strictly newer and is now the active
	// generation, so gen-old's succeeded rows are dead history.
	seedSupersedingActiveGeneration(t, ctx, db, "git:scope-a", "gen-new", base.Add(time.Hour))

	domain := string(reducer.DomainSupplyChainImpact)
	seedSucceededReopenWorkItem(t, ctx, db, "work-stale", "git:scope-a", "gen-old", domain, base)
	seedSucceededReopenWorkItem(t, ctx, db, "work-active", "git:scope-a", "gen-new", domain, base)
	// A scope that has never activated a generation must still reopen: this is
	// the activation race the replay exists for, not a superseded row.
	seedSucceededReopenWorkItem(t, ctx, db, "work-unactivated", "git:scope-b", "gen-b", domain, base)

	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return base }

	if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v", err)
	}

	if got, want := workItemStatus(t, ctx, db, "work-stale"), "succeeded"; got != want {
		t.Fatalf(
			"superseded-generation work item status = %q, want %q "+
				"(a generation with a strictly newer active sibling must not be replayed on every drain)",
			got, want,
		)
	}
	if got, want := workItemStatus(t, ctx, db, "work-active"), "pending"; got != want {
		t.Fatalf("active-generation work item status = %q, want %q", got, want)
	}
	if got, want := workItemStatus(t, ctx, db, "work-unactivated"), "pending"; got != want {
		t.Fatalf(
			"unactivated-scope work item status = %q, want %q "+
				"(NULL active_generation_id is the activation race, not supersession)",
			got, want,
		)
	}
}

// TestCrossScopeCorrelationReopenDomainsCoversDeclaredConsumers is the drift
// gate on the shared reopen list: every consumer domain the reducer's
// cross-scope dependency catalog declares must appear in it.
//
// The expectation is DERIVED from reducer.CrossScopeConsumerDomains(), not
// restated. The earlier form of this coverage claim hardcoded the same five
// constants the production list hardcodes and compared them to themselves, so
// the two could never disagree: adding a consumer to
// crossScopeDependencyCatalog without touching the reopen list still passed.
// That is the same silent under-replay this change fixes one level up — a
// consumer that waits on another scope's generation, with nothing to replay it,
// keeps its empty-join decision forever.
//
// The assertion is one-directional on purpose. Every declared CONSUMER must be
// reopened, because until the #5709 readiness-defer and activation re-enqueue
// slices land this reopen is the only thing that re-runs it. The reopen list is
// allowed to be a strict superset: deployable_unit_correlation and
// kubernetes_correlation_materialization wait on resolved relationships and
// cross-scope OCI manifest FACTS rather than on another reducer domain's
// output, so they are legitimately absent from a catalog that models only
// reducer-domain producers.
func TestCrossScopeCorrelationReopenDomainsCoversDeclaredConsumers(t *testing.T) {
	t.Parallel()

	reopened := CrossScopeCorrelationReopenDomains()
	for _, consumer := range reducer.CrossScopeConsumerDomains() {
		if !slices.Contains(reopened, string(consumer)) {
			t.Errorf(
				"cross-scope consumer domain %q declares a producer in crossScopeDependencyCatalog "+
					"but is not in CrossScopeCorrelationReopenDomains() = %v; nothing replays it after "+
					"the producer scope's generation activates, so it keeps its empty-join decision forever",
				consumer, reopened,
			)
		}
	}
}

// TestCrossScopeCorrelationReopenDomainsPinsListAndReturnsFreshSlice pins the
// exact contents and order of the shared reopen list, and its defensive copy.
// Both the ingester maintenance pass and eshu-bootstrap-index consume this one
// list, so a domain dropped here cannot be replayed by one runtime and not the
// other — the exact split the PR #5846 codex P1 reported. Coverage of the
// declared cross-scope chain is asserted separately, and derived, by
// TestCrossScopeCorrelationReopenDomainsCoversDeclaredConsumers.
func TestCrossScopeCorrelationReopenDomainsPinsListAndReturnsFreshSlice(t *testing.T) {
	t.Parallel()

	got := CrossScopeCorrelationReopenDomains()
	want := []string{
		string(reducer.DomainDeployableUnitCorrelation),
		string(reducer.DomainKubernetesCorrelationMaterialization),
		string(reducer.DomainContainerImageIdentity),
		string(reducer.DomainCICDRunCorrelation),
		string(reducer.DomainSupplyChainImpact),
	}
	if len(got) != len(want) {
		t.Fatalf("CrossScopeCorrelationReopenDomains() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CrossScopeCorrelationReopenDomains()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Mutating the returned slice must not corrupt the shared list for the next
	// caller: both runtimes call this on every maintenance pass.
	got[0] = "mutated"
	if CrossScopeCorrelationReopenDomains()[0] != string(reducer.DomainDeployableUnitCorrelation) {
		t.Fatal("CrossScopeCorrelationReopenDomains() returned an aliased slice; callers can corrupt the shared list")
	}
}

// TestListSucceededReducerWorkItemsByDomainQueryCarriesReplayFloor pins the SQL
// shape of the bound proven live by
// TestRunDeferredRelationshipMaintenanceSkipsSupersededCorrelationWorkItems and
// TestRunDeferredRelationshipMaintenanceBoundsScopesWithNoUsableActiveGeneration,
// so a future edit cannot quietly drop the floor (unbounded per-drain replay),
// its no-active fallback (the failed-scope and dangling-pointer churn holes), or
// the MATERIALIZED hint the listing's measured cost depends on.
func TestListSucceededReducerWorkItemsByDomainQueryCarriesReplayFloor(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"WITH scope_replay_floor AS MATERIALIZED",
		"COALESCE(active_generation.ingested_at, latest_generation.ingested_at)",
		"COALESCE(active_generation.generation_id, latest_generation.generation_id)",
		"ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC",
		">= (floor.floor_ingested_at, floor.floor_generation_id)",
	} {
		if !strings.Contains(listSucceededReducerWorkItemsByDomainQuery, fragment) {
			t.Fatalf("listSucceededReducerWorkItemsByDomainQuery missing %q; query = %s",
				fragment, listSucceededReducerWorkItemsByDomainQuery)
		}
	}
}

// TestRunDeferredRelationshipMaintenanceBoundsScopesWithNoUsableActiveGeneration
// is the regression proof for the generation-count-linear hole an
// "active_generation_id IS NOT NULL" guard leaves open (PR #5846 follow-up
// review, P1-2).
//
// Two production shapes reach this listing with no usable active generation and
// are NOT the activation race:
//
//   - failProjectorWorkQuery (projector_queue_sql.go) sets
//     active_generation_id = NULL when the ACTIVE generation fails. Under a
//     NULL-guarded exclusion every succeeded correlation row across ALL of that
//     scope's generations reopens on EVERY drain, forever — and
//     supersedeInactiveReducerGenerationsCTE carries the same guard, so nothing
//     ever terminalizes them.
//   - ingestion_scopes.active_generation_id has no foreign key
//     (schema/data-plane/postgres/001_ingestion_scopes.sql), so it can dangle at
//     a generation row that is gone, with the same effect.
//
// Both must collapse to the scope's LATEST generation: flat in generation count,
// and still replaying the one generation whose re-decision a query can read.
func TestRunDeferredRelationshipMaintenanceBoundsScopesWithNoUsableActiveGeneration(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-a", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, map[string]string{"repo-a": "beta-service"}, base)

	domain := string(reducer.DomainSupplyChainImpact)

	// A scope whose active generation failed: three generations, active nulled.
	seedScopeGeneration(t, ctx, db, "git:scope-failed", "gen-failed-1", base, false)
	seedScopeGeneration(t, ctx, db, "git:scope-failed", "gen-failed-2", base.Add(time.Hour), false)
	seedScopeGeneration(t, ctx, db, "git:scope-failed", "gen-failed-3", base.Add(2*time.Hour), false)
	for _, id := range []string{"1", "2", "3"} {
		seedSucceededReopenWorkItem(
			t, ctx, db, "work-failed-"+id, "git:scope-failed", "gen-failed-"+id, domain, base,
		)
	}

	// A scope whose active_generation_id dangles at a generation row that is gone.
	seedScopeGeneration(t, ctx, db, "git:scope-dangling", "gen-dangling-1", base, false)
	seedScopeGeneration(t, ctx, db, "git:scope-dangling", "gen-dangling-2", base.Add(time.Hour), false)
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1",
		"git:scope-dangling", "gen-deleted-by-retention"); err != nil {
		t.Fatalf("point scope-dangling at a missing generation: %v", err)
	}
	for _, id := range []string{"1", "2"} {
		seedSucceededReopenWorkItem(
			t, ctx, db, "work-dangling-"+id, "git:scope-dangling", "gen-dangling-"+id, domain, base,
		)
	}

	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return base }

	if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v", err)
	}

	for _, tc := range []struct {
		workItemID string
		want       string
		why        string
	}{
		{"work-failed-1", "succeeded", "a failed scope's older generations must not reopen on every drain"},
		{"work-failed-2", "succeeded", "a failed scope's older generations must not reopen on every drain"},
		{"work-failed-3", "pending", "the failed scope's LATEST generation is still worth replaying"},
		{"work-dangling-1", "succeeded", "a dangling active pointer must not reopen every generation"},
		{"work-dangling-2", "pending", "the dangling scope's LATEST generation must still replay"},
	} {
		if got := workItemStatus(t, ctx, db, tc.workItemID); got != tc.want {
			t.Fatalf("%s status = %q, want %q (%s)", tc.workItemID, got, tc.want, tc.why)
		}
	}
}

// seedScopeGeneration inserts one generation for a scope, creating the scope
// with a NULL active_generation_id if it does not exist yet, and optionally
// activating the generation.
func seedScopeGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID string,
	ingestedAt time.Time,
	activate bool,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL) ON CONFLICT DO NOTHING",
		scopeID); err != nil {
		t.Fatalf("seed scope %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		generationID, scopeID, ingestedAt); err != nil {
		t.Fatalf("seed generation %q: %v", generationID, err)
	}
	if !activate {
		return
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1",
		scopeID, generationID); err != nil {
		t.Fatalf("activate generation %q for scope %q: %v", generationID, scopeID, err)
	}
}

// seedSupersedingActiveGeneration inserts a strictly newer generation for the
// scope and points the scope's active_generation_id at it, reproducing a
// re-ingest that leaves the previous generation's succeeded reducer rows behind
// as dead history.
func seedSupersedingActiveGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID string,
	ingestedAt time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		generationID, scopeID, ingestedAt); err != nil {
		t.Fatalf("seed superseding generation %q: %v", generationID, err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1",
		scopeID, generationID); err != nil {
		t.Fatalf("activate generation %q for scope %q: %v", generationID, scopeID, err)
	}
}
