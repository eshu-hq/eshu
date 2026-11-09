// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"log/slog"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// SupplyChainAdvisoryGroupProjectionEnqueuer admits one
// advisory_group_projection intent per advisory group a committed pass
// touched (#10411). The production implementation writes durable reducer
// queue rows; a nil enqueuer leaves the graph projection to converge on
// the next touch or backfill.
type SupplyChainAdvisoryGroupProjectionEnqueuer interface {
	EnqueueAdvisoryGroupProjections(ctx context.Context, origin reducercontract.Intent, groupIDs []string) error
}

// enqueueAdvisoryGroupProjections asks the enqueuer to project every group
// this pass touched. It runs after the writer commit and the holder
// fan-out, and it never fails the pass: the AdvisoryGroup graph family is
// a cache over the winners read model, so a failed enqueue is counted and
// logged, and the group converges on its next touch or a backfill instead
// of rolling back committed findings.
func (h SupplyChainImpactHandler) enqueueAdvisoryGroupProjections(
	ctx context.Context,
	intent reducercontract.Intent,
	write SupplyChainImpactWriteResult,
) {
	if h.GroupProjectionEnqueuer == nil || len(write.TouchedGroupIDs) == 0 {
		return
	}
	if err := h.GroupProjectionEnqueuer.EnqueueAdvisoryGroupProjections(ctx, intent, write.TouchedGroupIDs); err != nil {
		h.Instruments.RecordAdvisoryGroupEnqueueFailure(ctx, string(reducercontract.DomainSupplyChainImpact))
		slog.WarnContext(
			ctx, "advisory group projection enqueue failed; groups converge on next touch",
			log.ScopeID(intent.ScopeID),
			log.GenerationID(intent.GenerationID),
			log.Domain(string(reducercontract.DomainSupplyChainImpact)),
			slog.Int("touched_group_count", len(write.TouchedGroupIDs)),
			slog.String("error", err.Error()),
		)
	}
}
