// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedServiceCompletesWithExtensionOutcomeIncludingUnchanged(t *testing.T) {
	now := time.Date(2026, time.October, 28, 23, 0, 0, 0, time.UTC)
	for _, unchanged := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial", true: "unchanged"}[unchanged], func(t *testing.T) {
			item := testClaimedWorkItem(now)
			claim := testWorkflowClaim(item.WorkItemID, now)
			store := &stubClaimStore{item: item, claim: claim, found: true}
			collected := FactsFromSlice(testScope(), testGeneration(now), nil)
			collected.Unchanged = unchanged
			collected.ExtensionOutcome = &workflow.ExtensionOutcome{
				State: "partial", Partial: true, WarningCount: 1,
			}
			source := &stubClaimedSource{collected: collected, ok: true}
			service := testClaimedService(now, claim, item.CollectorKind, store, source, &stubClaimedCommitter{})
			if err := service.processClaimed(context.Background(), item, claim); err != nil {
				t.Fatalf("processClaimed() error = %v", err)
			}
			if got := store.lastComplete.ExtensionOutcome; got == nil || !got.Partial || got.WarningCount != 1 {
				t.Fatalf("completed extension outcome = %#v, want partial with one warning", got)
			}
		})
	}
}
