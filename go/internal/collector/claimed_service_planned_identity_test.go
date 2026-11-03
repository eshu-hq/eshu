// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestClaimMutationCopiesPlannedItemIdentity proves every claim mutation
// carries the item's planned scope, source run, and generation (#9376 D8):
// the claim-plane commit pin matches the item row on those values, so a
// terraform-state commit that resolves a different generation must still pin
// with the planned candidate identity.
func TestClaimMutationCopiesPlannedItemIdentity(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.ScopeID = "scope-planned"
	item.SourceRunID = "tfstate-candidate:s3:abc"
	item.GenerationID = "tfstate-candidate:s3:abc"
	service := ClaimedService{CollectorKind: scope.CollectorTerraformState, ClaimLeaseTTL: time.Minute}

	mutation := service.claimMutation(item, testWorkflowClaim(item.WorkItemID, now))

	if mutation.ScopeID != item.ScopeID || mutation.SourceRunID != item.SourceRunID ||
		mutation.GenerationID != item.GenerationID {
		t.Fatalf("planned identity = (%q, %q, %q), want (%q, %q, %q)",
			mutation.ScopeID, mutation.SourceRunID, mutation.GenerationID,
			item.ScopeID, item.SourceRunID, item.GenerationID)
	}
}
