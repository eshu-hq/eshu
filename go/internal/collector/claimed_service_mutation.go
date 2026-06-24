// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s ClaimedService) claimMutation(item workflow.WorkItem, claim workflow.Claim) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:         item.WorkItemID,
		ClaimID:            claim.ClaimID,
		FencingToken:       claim.FencingToken,
		OwnerID:            claim.OwnerID,
		ObservedAt:         s.now(),
		LeaseDuration:      s.ClaimLeaseTTL,
		TenantID:           item.TenantID,
		WorkspaceID:        item.WorkspaceID,
		SubjectClass:       item.SubjectClass,
		PolicyRevisionHash: item.PolicyRevisionHash,
	}
}

func (s ClaimedService) resolvedCompletionMutation(
	mutation workflow.ClaimMutation,
	collected CollectedGeneration,
) (workflow.ClaimMutation, error) {
	if s.CollectorKind != scope.CollectorTerraformState {
		return mutation, nil
	}
	if err := collected.Generation.ValidateForScope(collected.Scope); err != nil {
		return workflow.ClaimMutation{}, fmt.Errorf("resolve terraform state claim identity: %w", err)
	}
	mutation.ResolvedScopeID = collected.Scope.ScopeID
	mutation.ResolvedAcceptanceUnitID = collected.Scope.ScopeID
	mutation.ResolvedSourceRunID = collected.Generation.GenerationID
	mutation.ResolvedGenerationID = collected.Generation.GenerationID
	return mutation, nil
}
