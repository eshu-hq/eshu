// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) claimMutation(item workflow.WorkItem, claim workflow.Claim) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:          item.WorkItemID,
		ClaimID:             claim.ClaimID,
		FencingToken:        claim.FencingToken,
		OwnerID:             claim.OwnerID,
		ObservedAt:          s.now(),
		LeaseDuration:       s.ClaimLeaseTTL,
		TenantID:            item.TenantID,
		WorkspaceID:         item.WorkspaceID,
		SubjectClass:        item.SubjectClass,
		PolicyRevisionHash:  item.PolicyRevisionHash,
		DerivedScannerImage: strings.HasPrefix(item.ScopeID, derivedImageScopePrefix),
	}
}
