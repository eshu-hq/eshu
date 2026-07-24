// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ObserveClaimedGenerationCommitted implements
// collector.ClaimedGenerationCommitObserver. collector.ClaimedService calls
// this exactly once, after item's generation has committed durably, before
// the claim is marked complete (#5429). It persists whatever watermark
// NextClaimed stashed for item's (ScopeID, GenerationID) in s.pending -- the
// durable write this method performs is the ONLY place the watermark
// advances; NextClaimed itself never writes it (see source.go's NextClaimed
// and pending_watermark.go).
//
// A missing stash entry (ok==false from takeFor) is a no-op, not an error:
// it means either NextClaimed's fetched window was empty (nothing to
// persist) or this call is for a work item this ClaimedSource never
// actually produced a generation for. saveWatermark itself stays nil-safe
// and fenced exactly as it always was -- only the TIMING of the call moved
// with this fix, not saveWatermark's own semantics.
func (s ClaimedSource) ObserveClaimedGenerationCommitted(ctx context.Context, item workflow.WorkItem) error {
	entry, ok := s.pending.takeFor(item)
	if !ok {
		return nil
	}
	return s.saveWatermark(ctx, item, entry.target, entry.newestRunID)
}
