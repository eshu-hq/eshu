package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const workflowControlIntegrationDSNEnv = "ESHU_POSTGRES_DSN"

func TestWorkflowControlStoreIntegrationHeartbeatReclaimAndSplitBrain(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 15, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-1",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-integration-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	item, claimA, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-a",
		ClaimID:             "claim-a",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}
	if got, want := claimA.FencingToken, int64(1); got != want {
		t.Fatalf("claimA.FencingToken = %d, want %d", got, want)
	}
	mustHeartbeatClaim(t, store, ctx, workflow.ClaimMutation{
		WorkItemID:    item.WorkItemID,
		ClaimID:       claimA.ClaimID,
		FencingToken:  claimA.FencingToken,
		OwnerID:       claimA.OwnerID,
		ObservedAt:    now.Add(10 * time.Second),
		LeaseDuration: time.Minute,
	})
	mustClaimState(t, db, claimA.ClaimID, workflow.ClaimStatusActive, claimA.FencingToken)
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusClaimed, claimA.ClaimID, claimA.FencingToken)

	reapAt := now.Add(2 * time.Minute)
	claims, err := store.ReapExpiredClaims(ctx, reapAt, 10, 0)
	if err != nil {
		t.Fatalf("ReapExpiredClaims() error = %v, want nil", err)
	}
	if got, want := len(claims), 1; got != want {
		t.Fatalf("len(claims) = %d, want %d", got, want)
	}
	if got, want := claims[0].ClaimID, claimA.ClaimID; got != want {
		t.Fatalf("reaped claim id = %q, want %q", got, want)
	}
	mustClaimState(t, db, claimA.ClaimID, workflow.ClaimStatusExpired, claimA.FencingToken)
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusPending, "", claimA.FencingToken)

	claimAfterReap := reapAt.Add(DefaultWorkflowExpiredClaimRequeueDelay + time.Second)
	item2, claimB, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-b",
		ClaimID:             "claim-b",
	}, claimAfterReap, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() after reap error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() after reap found = false, want true")
	}
	if got, want := item2.WorkItemID, item.WorkItemID; got != want {
		t.Fatalf("reclaimed work item = %q, want %q", got, want)
	}
	if got, want := claimB.FencingToken, claimA.FencingToken+1; got != want {
		t.Fatalf("claimB.FencingToken = %d, want %d", got, want)
	}

	staleErr := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID:    item.WorkItemID,
		ClaimID:       claimA.ClaimID,
		FencingToken:  claimA.FencingToken,
		OwnerID:       claimA.OwnerID,
		ObservedAt:    claimAfterReap.Add(time.Second),
		LeaseDuration: time.Minute,
	})
	if !errors.Is(staleErr, ErrWorkflowClaimRejected) {
		t.Fatalf("stale HeartbeatClaim() error = %v, want ErrWorkflowClaimRejected", staleErr)
	}
	staleErr = store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID:   item.WorkItemID,
		ClaimID:      claimA.ClaimID,
		FencingToken: claimA.FencingToken,
		OwnerID:      claimA.OwnerID,
		ObservedAt:   claimAfterReap.Add(2 * time.Second),
	})
	if !errors.Is(staleErr, ErrWorkflowClaimRejected) {
		t.Fatalf("stale CompleteClaim() error = %v, want ErrWorkflowClaimRejected", staleErr)
	}

	if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID:   item2.WorkItemID,
		ClaimID:      claimB.ClaimID,
		FencingToken: claimB.FencingToken,
		OwnerID:      claimB.OwnerID,
		ObservedAt:   claimAfterReap.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("CompleteClaim() error = %v, want nil", err)
	}
	mustClaimState(t, db, claimB.ClaimID, workflow.ClaimStatusCompleted, claimB.FencingToken)
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusCompleted, "", claimB.FencingToken)
}

func TestWorkflowControlStoreIntegrationReapExpiredClaimsLeavesActiveClaimsUntouched(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 16, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-2",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-2",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-integration-2",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-3",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-integration-3",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now.Add(time.Second),
		UpdatedAt:           now.Add(time.Second),
	})

	expiredItem, expiredClaim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-a",
		ClaimID:             "claim-expired",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}
	activeAt := now.Add(90 * time.Second)
	activeItem, activeClaim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-b",
		ClaimID:             "claim-active",
	}, activeAt, time.Minute)
	if err != nil {
		t.Fatalf("second ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("second ClaimNextEligible() found = false, want true")
	}

	markClaimExpired(t, db, expiredClaim.ClaimID, expiredItem.WorkItemID, expiredClaim.OwnerID, expiredClaim.FencingToken, now.Add(-time.Second))

	claims, err := store.ReapExpiredClaims(ctx, now.Add(2*time.Minute), 10, 0)
	if err != nil {
		t.Fatalf("ReapExpiredClaims() error = %v, want nil", err)
	}
	if got, want := len(claims), 1; got != want {
		t.Fatalf("len(claims) = %d, want %d", got, want)
	}
	if got, want := claims[0].ClaimID, expiredClaim.ClaimID; got != want {
		t.Fatalf("reaped claim id = %q, want %q", got, want)
	}

	mustClaimState(t, db, expiredClaim.ClaimID, workflow.ClaimStatusExpired, expiredClaim.FencingToken)
	mustWorkItemState(t, db, expiredItem.WorkItemID, workflow.WorkItemStatusPending, "", expiredClaim.FencingToken)
	mustClaimState(t, db, activeClaim.ClaimID, workflow.ClaimStatusActive, activeClaim.FencingToken)
	mustWorkItemState(t, db, activeItem.WorkItemID, workflow.WorkItemStatusClaimed, activeClaim.ClaimID, activeClaim.FencingToken)
}

func TestWorkflowControlStoreIntegrationClaimOrderRemainsFifoWithinCollectorInstance(t *testing.T) {
	_, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 17, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-3",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	for _, item := range []workflow.WorkItem{
		{
			WorkItemID:          "integration-item-fifo-1",
			RunID:               run.RunID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			ScopeID:             "scope-fifo-1",
			FairnessKey:         "zzz-late-lexical",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		{
			WorkItemID:          "integration-item-fifo-2",
			RunID:               run.RunID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			ScopeID:             "scope-fifo-2",
			FairnessKey:         "aaa-early-lexical",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now.Add(time.Second),
			UpdatedAt:           now.Add(time.Second),
		},
		{
			WorkItemID:          "integration-item-fifo-3",
			RunID:               run.RunID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			ScopeID:             "scope-fifo-3",
			FairnessKey:         "mmm-middle-lexical",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now.Add(2 * time.Second),
			UpdatedAt:           now.Add(2 * time.Second),
		},
	} {
		mustEnqueueWorkItem(t, store, ctx, item)
	}

	claimAt := now.Add(10 * time.Second)
	wantOrder := []string{
		"integration-item-fifo-1",
		"integration-item-fifo-2",
		"integration-item-fifo-3",
	}
	for i, wantWorkItemID := range wantOrder {
		item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			OwnerID:             fmt.Sprintf("collector-pod-fifo-%d", i+1),
			ClaimID:             fmt.Sprintf("claim-fifo-%d", i+1),
		}, claimAt.Add(time.Duration(i)*time.Second), time.Minute)
		if err != nil {
			t.Fatalf("ClaimNextEligible() #%d error = %v, want nil", i+1, err)
		}
		if !found {
			t.Fatalf("ClaimNextEligible() #%d found = false, want true", i+1)
		}
		if got := item.WorkItemID; got != wantWorkItemID {
			t.Fatalf("ClaimNextEligible() #%d work item = %q, want %q", i+1, got, wantWorkItemID)
		}
		if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
			WorkItemID:   item.WorkItemID,
			ClaimID:      claim.ClaimID,
			FencingToken: claim.FencingToken,
			OwnerID:      claim.OwnerID,
			ObservedAt:   claimAt.Add(time.Duration(i+1) * time.Second),
		}); err != nil {
			t.Fatalf("CompleteClaim() #%d error = %v, want nil", i+1, err)
		}
	}

	_, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-fifo-final",
		ClaimID:             "claim-fifo-final",
	}, claimAt.Add(10*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("final ClaimNextEligible() error = %v, want nil", err)
	}
	if found {
		t.Fatal("final ClaimNextEligible() found = true, want false")
	}
}

func TestWorkflowControlStoreIntegrationReconcileWorkflowRunsUsesReducerPhaseTruth(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 19, 0, 0, 0, time.UTC)
	scopeValue := scope.IngestionScope{
		ScopeID:       "git-repository-scope:integration-workflow-run",
		SourceSystem:  "github",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "integration-workflow-run",
		Metadata: map[string]string{
			"repo_id":    "integration-repo-1",
			"source_key": "integration-repo-1",
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-integration-workflow-run",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusCompleted,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	mustUpsertScopeBoundary(t, db, scopeValue, generation)

	run := workflow.Run{
		RunID:       "integration-run-complete",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusReducerConverging,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-complete",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		SourceSystem:        scopeValue.SourceSystem,
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "integration-repo-1",
		SourceRunID:         generation.GenerationID,
		GenerationID:        generation.GenerationID,
		Status:              workflow.WorkItemStatusCompleted,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	phaseStore := NewGraphProjectionPhaseStateStore(SQLDB{DB: db})
	if err := phaseStore.Upsert(ctx, []reducer.GraphProjectionPhaseState{
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
			},
			Phase:       reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
			},
			Phase:       reducer.GraphProjectionPhaseSemanticNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceDeployableUnitUID,
			},
			Phase:       reducer.GraphProjectionPhaseDeployableUnitCorrelation,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceServiceUID,
			},
			Phase:       reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceServiceUID,
			},
			Phase:       reducer.GraphProjectionPhaseDeploymentMapping,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceServiceUID,
			},
			Phase:       reducer.GraphProjectionPhaseWorkloadMaterialization,
			CommittedAt: now,
			UpdatedAt:   now,
		},
	}); err != nil {
		t.Fatalf("phaseStore.Upsert() error = %v, want nil", err)
	}

	reconciled, err := store.ReconcileWorkflowRuns(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	mustWorkflowRunStatus(t, db, run.RunID, workflow.RunStatusComplete)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
		"canonical_nodes_committed",
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
		"semantic_nodes_committed",
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(reducer.GraphProjectionKeyspaceDeployableUnitUID),
		string(reducer.GraphProjectionPhaseDeployableUnitCorrelation),
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(reducer.GraphProjectionKeyspaceServiceUID),
		string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(reducer.GraphProjectionKeyspaceServiceUID),
		string(reducer.GraphProjectionPhaseDeploymentMapping),
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(reducer.GraphProjectionKeyspaceServiceUID),
		string(reducer.GraphProjectionPhaseWorkloadMaterialization),
		workflow.CompletenessStatusReady,
	)
}

func TestWorkflowControlStoreIntegrationHeartbeatUpdatesClaimAndWorkItemLeaseTogether(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-4",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-heartbeat-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-heartbeat-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-heartbeat",
		ClaimID:             "claim-heartbeat",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}

	heartbeatAt := now.Add(15 * time.Second)
	leaseTTL := 90 * time.Second
	if err := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID:    item.WorkItemID,
		ClaimID:       claim.ClaimID,
		FencingToken:  claim.FencingToken,
		OwnerID:       claim.OwnerID,
		ObservedAt:    heartbeatAt,
		LeaseDuration: leaseTTL,
	}); err != nil {
		t.Fatalf("HeartbeatClaim() error = %v, want nil", err)
	}

	mustHeartbeatLeaseState(t, db, claim.ClaimID, item.WorkItemID, heartbeatAt, heartbeatAt.Add(leaseTTL))
}

func openWorkflowControlIntegrationStore(t *testing.T) (*sql.DB, *WorkflowControlStore) {
	t.Helper()

	dsn := os.Getenv(workflowControlIntegrationDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping Postgres integration test", workflowControlIntegrationDSNEnv)
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("PingContext() error = %v, want nil", err)
	}

	store := NewWorkflowControlStore(SQLDB{DB: db})
	if err := store.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("EnsureSchema() error = %v, want nil", err)
	}
	if _, err := db.ExecContext(ctx, `
TRUNCATE workflow_claims, workflow_work_items, workflow_runs, collector_instances, workflow_run_completeness
RESTART IDENTITY CASCADE
`); err != nil {
		_ = db.Close()
		t.Fatalf("TRUNCATE workflow control tables error = %v, want nil", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db, store
}

func mustCreateRun(t *testing.T, store *WorkflowControlStore, ctx context.Context, run workflow.Run) {
	t.Helper()
	if err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun() error = %v, want nil", err)
	}
}

func mustUpsertScopeBoundary(t *testing.T, db *sql.DB, scopeValue scope.IngestionScope, generation scope.ScopeGeneration) {
	t.Helper()
	if err := upsertIngestionScope(context.Background(), SQLDB{DB: db}, scopeValue, generation); err != nil {
		t.Fatalf("upsertIngestionScope() error = %v, want nil", err)
	}
	if err := upsertScopeGeneration(context.Background(), SQLDB{DB: db}, generation); err != nil {
		t.Fatalf("upsertScopeGeneration() error = %v, want nil", err)
	}
}

func mustEnqueueWorkItem(t *testing.T, store *WorkflowControlStore, ctx context.Context, item workflow.WorkItem) {
	t.Helper()
	if item.SourceSystem == "" {
		item.SourceSystem = string(item.CollectorKind)
	}
	if item.GenerationID == "" {
		item.GenerationID = item.WorkItemID + "-generation"
	}
	if item.SourceRunID == "" {
		item.SourceRunID = item.GenerationID
	}
	if item.AcceptanceUnitID == "" {
		item.AcceptanceUnitID = item.ScopeID
	}
	if err := store.EnqueueWorkItems(ctx, []workflow.WorkItem{item}); err != nil {
		t.Fatalf("EnqueueWorkItems() error = %v, want nil", err)
	}
}

func mustHeartbeatClaim(t *testing.T, store *WorkflowControlStore, ctx context.Context, mutation workflow.ClaimMutation) {
	t.Helper()
	if err := store.HeartbeatClaim(ctx, mutation); err != nil {
		t.Fatalf("HeartbeatClaim() error = %v, want nil", err)
	}
}

func mustClaimState(t *testing.T, db *sql.DB, claimID string, wantStatus workflow.ClaimStatus, wantFence int64) {
	t.Helper()

	var gotStatus string
	var gotFence int64
	if err := db.QueryRowContext(context.Background(), `
SELECT status, fencing_token
FROM workflow_claims
WHERE claim_id = $1
`, claimID).Scan(&gotStatus, &gotFence); err != nil {
		t.Fatalf("query claim %q error = %v, want nil", claimID, err)
	}
	if gotStatus != string(wantStatus) {
		t.Fatalf("claim %q status = %q, want %q", claimID, gotStatus, wantStatus)
	}
	if gotFence != wantFence {
		t.Fatalf("claim %q fencing_token = %d, want %d", claimID, gotFence, wantFence)
	}
}

func mustHeartbeatLeaseState(
	t *testing.T,
	db *sql.DB,
	claimID string,
	workItemID string,
	wantHeartbeatAt time.Time,
	wantLeaseExpiresAt time.Time,
) {
	t.Helper()

	var gotClaimHeartbeatAt time.Time
	var gotClaimLeaseExpiresAt time.Time
	if err := db.QueryRowContext(context.Background(), `
SELECT heartbeat_at, lease_expires_at
FROM workflow_claims
WHERE claim_id = $1
`, claimID).Scan(&gotClaimHeartbeatAt, &gotClaimLeaseExpiresAt); err != nil {
		t.Fatalf("query heartbeat claim %q error = %v, want nil", claimID, err)
	}
	if !gotClaimHeartbeatAt.Equal(wantHeartbeatAt) {
		t.Fatalf("claim %q heartbeat_at = %v, want %v", claimID, gotClaimHeartbeatAt, wantHeartbeatAt)
	}
	if !gotClaimLeaseExpiresAt.Equal(wantLeaseExpiresAt) {
		t.Fatalf("claim %q lease_expires_at = %v, want %v", claimID, gotClaimLeaseExpiresAt, wantLeaseExpiresAt)
	}

	var gotWorkItemLeaseExpiresAt time.Time
	if err := db.QueryRowContext(context.Background(), `
SELECT lease_expires_at
FROM workflow_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&gotWorkItemLeaseExpiresAt); err != nil {
		t.Fatalf("query work item %q lease error = %v, want nil", workItemID, err)
	}
	if !gotWorkItemLeaseExpiresAt.Equal(wantLeaseExpiresAt) {
		t.Fatalf("work item %q lease_expires_at = %v, want %v", workItemID, gotWorkItemLeaseExpiresAt, wantLeaseExpiresAt)
	}
}

func mustWorkflowRunStatus(t *testing.T, db *sql.DB, runID string, wantStatus workflow.RunStatus) {
	t.Helper()
	var gotStatus string
	if err := db.QueryRowContext(context.Background(), `
SELECT status
FROM workflow_runs
WHERE run_id = $1
`, runID).Scan(&gotStatus); err != nil {
		t.Fatalf("query workflow run %q error = %v, want nil", runID, err)
	}
	if gotStatus != string(wantStatus) {
		t.Fatalf("workflow run %q status = %q, want %q", runID, gotStatus, wantStatus)
	}
}

func mustCompletenessStatus(
	t *testing.T,
	db *sql.DB,
	runID string,
	collectorKind string,
	keyspace string,
	phaseName string,
	wantStatus string,
) {
	t.Helper()
	var gotStatus string
	if err := db.QueryRowContext(context.Background(), `
SELECT status
FROM workflow_run_completeness
WHERE run_id = $1
  AND collector_kind = $2
  AND keyspace = $3
  AND phase_name = $4
`, runID, collectorKind, keyspace, phaseName).Scan(&gotStatus); err != nil {
		t.Fatalf("query workflow completeness %q/%q/%q error = %v, want nil", collectorKind, keyspace, phaseName, err)
	}
	if gotStatus != wantStatus {
		t.Fatalf("workflow completeness %q/%q/%q status = %q, want %q", collectorKind, keyspace, phaseName, gotStatus, wantStatus)
	}
}

func mustWorkItemState(t *testing.T, db *sql.DB, workItemID string, wantStatus workflow.WorkItemStatus, wantClaimID string, wantFence int64) {
	t.Helper()

	var gotStatus, gotClaimID string
	var gotFence int64
	if err := db.QueryRowContext(context.Background(), `
SELECT status, COALESCE(current_claim_id, ''), current_fencing_token
FROM workflow_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&gotStatus, &gotClaimID, &gotFence); err != nil {
		t.Fatalf("query work item %q error = %v, want nil", workItemID, err)
	}
	if gotStatus != string(wantStatus) {
		t.Fatalf("work item %q status = %q, want %q", workItemID, gotStatus, wantStatus)
	}
	if gotClaimID != wantClaimID {
		t.Fatalf("work item %q current_claim_id = %q, want %q", workItemID, gotClaimID, wantClaimID)
	}
	if gotFence != wantFence {
		t.Fatalf("work item %q current_fencing_token = %d, want %d", workItemID, gotFence, wantFence)
	}
}

func markClaimExpired(t *testing.T, db *sql.DB, claimID, workItemID, ownerID string, fencingToken int64, expiredAt time.Time) {
	t.Helper()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
UPDATE workflow_claims
SET lease_expires_at = $1,
    updated_at = $1
WHERE claim_id = $2
  AND work_item_id = $3
  AND owner_id = $4
  AND fencing_token = $5
`, expiredAt, claimID, workItemID, ownerID, fencingToken); err != nil {
		t.Fatalf("expire claim %q error = %v, want nil", claimID, err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE workflow_work_items
SET lease_expires_at = $1,
    updated_at = $1
WHERE work_item_id = $2
  AND current_claim_id = $3
  AND current_owner_id = $4
  AND current_fencing_token = $5
`, expiredAt, workItemID, claimID, ownerID, fencingToken); err != nil {
		t.Fatalf("expire work item %q error = %v, want nil", workItemID, err)
	}
}
