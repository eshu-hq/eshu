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
// queue rows. A nil enqueuer skips the enqueue silently; production
// wiring always sets it (cmd/reducer), and the wiring test pins that.
type SupplyChainAdvisoryGroupProjectionEnqueuer interface {
	EnqueueAdvisoryGroupProjections(ctx context.Context, origin reducercontract.Intent, groupIDs []string) error
}

// enqueueAdvisoryGroupProjections asks the enqueuer to project every
// group this pass touched. It runs after the writer commit and BEFORE
// the holder fan-out: the fan-out clears the owed marker the writer tx
// recorded, so a failed enqueue must fail the pass while the marker
// still stands. The retry is then an owed rerun whose writer reports
// the scope's recovery groups, re-enqueueing every group even when the
// rewrite itself shows no change. Acking past a failed enqueue would
// lose the touch, because no second producer re-derives it.
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
