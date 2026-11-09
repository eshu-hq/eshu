// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// SupplyChainAdvisoryGroupProjectionEnqueuer admits one
// advisory_group_projection intent per advisory group a committed pass
// touched (#10411). The production implementation writes durable reducer
// queue rows. A nil enqueuer leaves the domain unregistered at wiring
// time, so a registered supply_chain_impact handler never silently
// carries a dead seam in production.
type SupplyChainAdvisoryGroupProjectionEnqueuer interface {
	EnqueueAdvisoryGroupProjections(ctx context.Context, origin reducercontract.Intent, groupIDs []string) error
}

// enqueueAdvisoryGroupProjections asks the enqueuer to project every
// group this pass touched. It runs after the writer commit and the
// holder fan-out. A failure fails the pass with a retryable error, the
// fan-out's own contract: acking past a failed enqueue would lose the
// touch, because no backfill or second producer re-derives it. The
// retry is safe — the writer tx recorded the fan-out owed marker for
// the pass (it covers touched groups as well as changed CVEs), so the
// owed rerun re-enqueues the scope's recovery groups even when its own
// rewrite shows no change.
func (h SupplyChainImpactHandler) enqueueAdvisoryGroupProjections(
	ctx context.Context,
	intent reducercontract.Intent,
	write SupplyChainImpactWriteResult,
) error {
	if h.GroupProjectionEnqueuer == nil || len(write.TouchedGroupIDs) == 0 {
		return nil
	}
	if err := h.GroupProjectionEnqueuer.EnqueueAdvisoryGroupProjections(ctx, intent, write.TouchedGroupIDs); err != nil {
		h.Instruments.RecordAdvisoryGroupEnqueueFailure(ctx, string(reducercontract.DomainSupplyChainImpact))
		return RetryableFanOutError(fmt.Errorf("enqueue advisory group projections: %w", err))
	}
	return nil
}
