package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreCreateRunExecutesInsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "run-1",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO workflow_runs") {
		t.Fatalf("query missing workflow_runs insert: %s", db.execs[0].query)
	}
	if strings.Contains(db.execs[0].query, "status = EXCLUDED.status") {
		t.Fatalf("query regresses existing workflow status on conflict: %s", db.execs[0].query)
	}
}

func TestWorkflowControlStoreEnqueueWorkItemsExecutesBatchInsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)
	items := []workflow.WorkItem{
		{
			WorkItemID:          "item-1",
			RunID:               "run-1",
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			SourceSystem:        "git",
			ScopeID:             "scope-1",
			AcceptanceUnitID:    "repository:scope-1",
			SourceRunID:         "source-run-1",
			GenerationID:        "generation-1",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		{
			WorkItemID:          "item-2",
			RunID:               "run-1",
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			SourceSystem:        "git",
			ScopeID:             "scope-2",
			AcceptanceUnitID:    "repository:scope-2",
			SourceRunID:         "source-run-2",
			GenerationID:        "generation-2",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
	}

	if err := store.EnqueueWorkItems(context.Background(), items); err != nil {
		t.Fatalf("EnqueueWorkItems() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO workflow_work_items") {
		t.Fatalf("query missing workflow_work_items insert: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[0].query, "ON CONFLICT DO NOTHING") {
		t.Fatalf("query must ignore all workflow work item uniqueness conflicts: %s", db.execs[0].query)
	}
	for _, want := range []string{"source_system", "acceptance_unit_id", "source_run_id"} {
		if !strings.Contains(db.execs[0].query, want) {
			t.Fatalf("query missing workflow identity column %q: %s", want, db.execs[0].query)
		}
	}
}

func TestWorkflowControlStoreClaimNextEligibleReturnsClaimAndWorkItem(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"item-1",
				"run-1",
				string(scope.CollectorGit),
				"collector-git-default",
				"git",
				"scope-1",
				"repository:scope-1",
				"source-run-1",
				"gen-1",
				"family:git",
				"claimed",
				1,
				"claim-1",
				sql.NullInt64{Int64: 1, Valid: true},
				"collector-pod-1",
				expiresAt,
				now,
				now,
				"claim-1",
				sql.NullInt64{Int64: 1, Valid: true},
				"collector-pod-1",
				"active",
				now,
				now,
				expiresAt,
				now,
				now,
			}}},
		},
	}
	store := NewWorkflowControlStore(db)

	item, claim, found, err := store.ClaimNextEligible(
		context.Background(),
		ClaimSelector{
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			OwnerID:             "collector-pod-1",
			ClaimID:             "claim-1",
		},
		now,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}
	if got, want := item.WorkItemID, "item-1"; got != want {
		t.Fatalf("item.WorkItemID = %q, want %q", got, want)
	}
	if got, want := item.SourceSystem, "git"; got != want {
		t.Fatalf("item.SourceSystem = %q, want %q", got, want)
	}
	if got, want := item.AcceptanceUnitID, "repository:scope-1"; got != want {
		t.Fatalf("item.AcceptanceUnitID = %q, want %q", got, want)
	}
	if got, want := item.SourceRunID, "source-run-1"; got != want {
		t.Fatalf("item.SourceRunID = %q, want %q", got, want)
	}
	if got, want := claim.FencingToken, int64(1); got != want {
		t.Fatalf("claim.FencingToken = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "FOR UPDATE SKIP LOCKED") {
		t.Fatalf("query missing SKIP LOCKED claim issuance: %s", db.queries[0].query)
	}
}

func TestWorkflowControlStoreClaimNextEligibleUsesDeterministicFifoWithinFamily(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"item-1",
				"run-1",
				string(scope.CollectorGit),
				"collector-git-default",
				"git",
				"scope-1",
				"repository:scope-1",
				"source-run-1",
				"gen-1",
				"",
				"claimed",
				1,
				"claim-1",
				sql.NullInt64{Int64: 1, Valid: true},
				"collector-pod-1",
				time.Date(2026, time.April, 20, 14, 1, 0, 0, time.UTC),
				time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
				time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
				"claim-1",
				sql.NullInt64{Int64: 1, Valid: true},
				"collector-pod-1",
				"active",
				time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
				time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
				time.Date(2026, time.April, 20, 14, 1, 0, 0, time.UTC),
				time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
				time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
			}}},
		},
	}
	store := NewWorkflowControlStore(db)

	_, _, found, err := store.ClaimNextEligible(
		context.Background(),
		ClaimSelector{
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			OwnerID:             "collector-pod-1",
			ClaimID:             "claim-1",
		},
		time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"ORDER BY COALESCE(visible_at, created_at), created_at, work_item_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing proof guard %q:\n%s", want, query)
		}
	}
}

func TestWorkflowControlStoreClaimNextEligibleUsesDefaultLeaseTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{
			"item-1",
			"run-1",
			string(scope.CollectorGit),
			"collector-git-default",
			"git",
			"scope-1",
			"repository:scope-1",
			"source-run-1",
			"",
			"",
			"claimed",
			1,
			"claim-1",
			sql.NullInt64{Int64: 1, Valid: true},
			"collector-pod-1",
			now.Add(DefaultWorkflowClaimLeaseTTL),
			now,
			now,
			"claim-1",
			sql.NullInt64{Int64: 1, Valid: true},
			"collector-pod-1",
			"active",
			now,
			now,
			now.Add(DefaultWorkflowClaimLeaseTTL),
			now,
			now,
		}}}},
	}
	store := NewWorkflowControlStore(db)

	_, _, _, err := store.ClaimNextEligible(context.Background(), ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-1",
		ClaimID:             "claim-1",
	}, now, 0)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if got, want := db.queries[0].args[5].(time.Time), now.Add(DefaultWorkflowClaimLeaseTTL); !got.Equal(want) {
		t.Fatalf("lease expiration = %v, want %v", got, want)
	}
}

func TestWorkflowControlStoreClaimNextEligibleRejectsInvalidLeaseSettings(t *testing.T) {
	t.Parallel()

	store := NewWorkflowControlStore(&fakeExecQueryer{})
	store.DefaultClaimLeaseTTL = DefaultWorkflowClaimLeaseTTL
	store.DefaultHeartbeatInterval = DefaultWorkflowClaimLeaseTTL

	_, _, _, err := store.ClaimNextEligible(context.Background(), ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-1",
		ClaimID:             "claim-1",
	}, time.Now().UTC(), 0)
	if err == nil {
		t.Fatal("ClaimNextEligible() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "heartbeat interval") {
		t.Fatalf("error = %q, want heartbeat interval validation", err.Error())
	}
}

func TestWorkflowControlStoreHeartbeatClaimUsesFencingAndOwnerGuards(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)

	err := store.HeartbeatClaim(context.Background(), ClaimMutation{
		WorkItemID:    "item-1",
		ClaimID:       "claim-1",
		FencingToken:  2,
		OwnerID:       "collector-pod-1",
		ObservedAt:    now,
		LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("HeartbeatClaim() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE workflow_claims",
		"fencing_token = $3",
		"owner_id = $4",
		"status = 'active'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("heartbeat query missing %q: %s", want, query)
		}
	}
}

func TestWorkflowControlStoreHeartbeatClaimRejectsStaleClaim(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}}}
	store := NewWorkflowControlStore(db)

	err := store.HeartbeatClaim(context.Background(), ClaimMutation{
		WorkItemID:    "item-1",
		ClaimID:       "claim-1",
		FencingToken:  2,
		OwnerID:       "collector-pod-1",
		ObservedAt:    time.Now().UTC(),
		LeaseDuration: time.Minute,
	})
	if err == nil {
		t.Fatal("HeartbeatClaim() error = nil, want non-nil")
	}
	if err != ErrWorkflowClaimRejected {
		t.Fatalf("HeartbeatClaim() error = %v, want ErrWorkflowClaimRejected", err)
	}
}

func TestWorkflowControlStoreCompleteClaimUsesFencingAndOwnerGuards(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)

	err := store.CompleteClaim(context.Background(), ClaimMutation{
		WorkItemID:   "item-1",
		ClaimID:      "claim-1",
		FencingToken: 2,
		OwnerID:      "collector-pod-1",
		ObservedAt:   now,
	})
	if err != nil {
		t.Fatalf("CompleteClaim() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE workflow_claims",
		"status = 'completed'",
		"fencing_token = $3",
		"owner_id = $4",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("complete query missing %q: %s", want, query)
		}
	}
}

func TestWorkflowControlStoreCompleteClaimRejectsStaleClaim(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}}}
	store := NewWorkflowControlStore(db)

	err := store.CompleteClaim(context.Background(), ClaimMutation{
		WorkItemID:   "item-1",
		ClaimID:      "claim-1",
		FencingToken: 2,
		OwnerID:      "collector-pod-1",
		ObservedAt:   time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("CompleteClaim() error = nil, want non-nil")
	}
	if err != ErrWorkflowClaimRejected {
		t.Fatalf("CompleteClaim() error = %v, want ErrWorkflowClaimRejected", err)
	}
}

func TestWorkflowControlStoreReapExpiredClaimsUsesSkipLockedAndBackoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"claim-1",
				"item-1",
				sql.NullInt64{Int64: 3, Valid: true},
				"collector-pod-1",
				"expired",
				now.Add(-time.Minute),
				now.Add(-time.Second),
				now.Add(-time.Second),
				now.Add(-time.Minute),
				now,
			}}},
		},
	}
	store := NewWorkflowControlStore(db)

	claims, err := store.ReapExpiredClaims(context.Background(), now, 10, 0)
	if err != nil {
		t.Fatalf("ReapExpiredClaims() error = %v, want nil", err)
	}
	if got, want := len(claims), 1; got != want {
		t.Fatalf("len(claims) = %d, want %d", got, want)
	}
	if got, want := claims[0].FencingToken, int64(3); got != want {
		t.Fatalf("claims[0].FencingToken = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FOR UPDATE OF claim, item SKIP LOCKED",
		"status = 'expired'",
		"status = 'claimed'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("reap query missing %q: %s", want, query)
		}
	}
	if got, want := db.queries[0].args[2].(time.Time), now.Add(DefaultWorkflowExpiredClaimRequeueDelay); !got.Equal(want) {
		t.Fatalf("reap visible_at = %v, want %v", got, want)
	}
}

func TestWorkflowControlBootstrapDefinitionRegistered(t *testing.T) {
	t.Parallel()

	var found bool
	for _, def := range BootstrapDefinitions() {
		if def.Name == "workflow_control_plane" {
			found = true
			if !strings.Contains(def.SQL, "workflow_runs") {
				t.Fatal("definition SQL missing workflow_runs")
			}
			break
		}
	}
	if !found {
		t.Fatal("workflow_control_plane not found in BootstrapDefinitions()")
	}
}
