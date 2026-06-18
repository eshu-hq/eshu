package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestIngestionStoreCommitClaimedScopeGenerationLocksTenantGrantBeforeFactWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 13, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	err := store.CommitClaimedScopeGeneration(
		context.Background(),
		tenantGrantMutation(now),
		tenantGrantScope(),
		tenantGrantGeneration(now),
		testFactChannel([]facts.Envelope{tenantGrantFact(now)}),
	)
	if err != nil {
		t.Fatalf("CommitClaimedScopeGeneration() error = %v, want nil", err)
	}
	query := db.tx.execs[0].query
	for _, want := range []string{
		"tenant_scope_grants",
		"tenants",
		"workspaces",
		"tenant_scope_grants.policy_revision_hash = item.policy_revision_hash",
		"tenants.status = 'active'",
		"workspaces.status = 'active'",
		"tenants.tombstoned_at IS NULL",
		"workspaces.tombstoned_at IS NULL",
		"tenant_scope_grants.tombstoned_at IS NULL",
		"tenant_scope_grants.effective_at <= $1",
		"(tenant_scope_grants.expires_at IS NULL OR tenant_scope_grants.expires_at > $1)",
		"FOR SHARE OF tenant_scope_grants, tenants, workspaces",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("tenant grant heartbeat query missing %q:\n%s", want, query)
		}
	}
	if got := db.tx.execs[0].query; !strings.Contains(got, "workflow_claims") {
		t.Fatalf("first exec query = %q, want heartbeat claim fence before grant lock", got)
	}
	if got := db.tx.execs[4].query; !strings.Contains(got, "INSERT INTO fact_records") {
		t.Fatalf("fifth exec query = %q, want fact write after grant lock and maintenance barrier", got)
	}
	if !db.tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
}

func TestValidateClaimMutationTenantBoundaryRejectsPartialBoundary(t *testing.T) {
	t.Parallel()

	mutation := tenantGrantMutation(time.Date(2026, time.June, 10, 13, 10, 0, 0, time.UTC))
	mutation.PolicyRevisionHash = ""

	boundarySet, err := validateClaimMutationTenantBoundary(mutation)
	if err == nil {
		t.Fatal("validateClaimMutationTenantBoundary() error = nil, want error")
	}
	if boundarySet {
		t.Fatal("validateClaimMutationTenantBoundary() boundarySet = true, want false")
	}
}

func TestValidateClaimMutationTenantBoundaryAllowsLegacySharedMode(t *testing.T) {
	t.Parallel()

	boundarySet, err := validateClaimMutationTenantBoundary(workflow.ClaimMutation{})
	if err != nil {
		t.Fatalf("validateClaimMutationTenantBoundary() error = %v, want nil", err)
	}
	if boundarySet {
		t.Fatal("validateClaimMutationTenantBoundary() boundarySet = true, want false")
	}
}

func tenantGrantMutation(now time.Time) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:         "work-item-tenant-grant",
		ClaimID:            "claim-tenant-grant",
		FencingToken:       11,
		OwnerID:            "collector-owner",
		ObservedAt:         now,
		LeaseDuration:      time.Minute,
		TenantID:           "tenant-a",
		WorkspaceID:        "workspace-a",
		SubjectClass:       "collector",
		PolicyRevisionHash: "policy-a",
	}
}

func tenantGrantScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-tenant-grant",
		SourceSystem:  "aws",
		ScopeKind:     scope.KindAccount,
		CollectorKind: scope.CollectorAWS,
		PartitionKey:  "aws:tenant-grant",
	}
}

func tenantGrantGeneration(now time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "generation-tenant-grant",
		ScopeID:      "scope-tenant-grant",
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func tenantGrantFact(now time.Time) facts.Envelope {
	return facts.Envelope{
		FactID:        "fact-tenant-grant",
		ScopeID:       "scope-tenant-grant",
		GenerationID:  "generation-tenant-grant",
		FactKind:      "aws_resource",
		StableFactKey: "aws-resource:tenant-grant",
		ObservedAt:    now,
		Payload:       map[string]any{"resource_type": "lambda"},
		SourceRef: facts.Ref{
			SourceSystem: "aws",
			FactKey:      "aws-resource:tenant-grant",
		},
	}
}
