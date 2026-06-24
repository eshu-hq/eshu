// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreIntegrationCompleteClaimCanResolveProjectionIdentity(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.May, 21, 14, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "integration-run-resolve-tfstate-identity",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionActive,
		RequestedCollector: string(scope.CollectorTerraformState),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	mustCreateRun(t, store, ctx, run)
	item := workflow.WorkItem{
		WorkItemID:          "integration-item-resolve-tfstate-identity",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             "state_snapshot:s3:locator-hash",
		AcceptanceUnitID:    "terraform_state:s3:locator-hash",
		SourceRunID:         "terraform_state_candidate:s3:candidate-hash",
		GenerationID:        "terraform_state_candidate:s3:candidate-hash",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	mustEnqueueWorkItem(t, store, ctx, item)
	claimed, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: item.CollectorInstanceID,
		OwnerID:             "collector-owner-1",
		ClaimID:             "claim-resolve-tfstate-identity",
	}, now.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}
	resolvedGenerationID := "terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17"
	if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID:               claimed.WorkItemID,
		ClaimID:                  claim.ClaimID,
		FencingToken:             claim.FencingToken,
		OwnerID:                  claim.OwnerID,
		ObservedAt:               now.Add(2 * time.Second),
		ResolvedScopeID:          item.ScopeID,
		ResolvedAcceptanceUnitID: item.ScopeID,
		ResolvedSourceRunID:      resolvedGenerationID,
		ResolvedGenerationID:     resolvedGenerationID,
	}); err != nil {
		t.Fatalf("CompleteClaim() error = %v, want nil", err)
	}

	var gotScopeID, gotAcceptanceUnitID, gotSourceRunID, gotGenerationID string
	if err := db.QueryRowContext(ctx, `
SELECT scope_id, acceptance_unit_id, source_run_id, generation_id
FROM workflow_work_items
WHERE work_item_id = $1
`, item.WorkItemID).Scan(&gotScopeID, &gotAcceptanceUnitID, &gotSourceRunID, &gotGenerationID); err != nil {
		t.Fatalf("query resolved work item identity error = %v, want nil", err)
	}
	if gotScopeID != item.ScopeID ||
		gotAcceptanceUnitID != item.ScopeID ||
		gotSourceRunID != resolvedGenerationID ||
		gotGenerationID != resolvedGenerationID {
		t.Fatalf(
			"resolved identity = (%q, %q, %q, %q), want (%q, %q, %q, %q)",
			gotScopeID,
			gotAcceptanceUnitID,
			gotSourceRunID,
			gotGenerationID,
			item.ScopeID,
			item.ScopeID,
			resolvedGenerationID,
			resolvedGenerationID,
		)
	}
}
