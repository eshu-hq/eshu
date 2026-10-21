// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// semanticEntityRepairQueueAdapter adapts gpphase.PhaseRepairQueue to
// semantic.GraphProjectionPhaseRepairQueue.
//
// The two repair structs are field-for-field identical, but code/semantic
// declares its own GraphProjectionPhaseRepair struct locally (issue #6061,
// see code/semantic/graph_ports.go) rather than importing gpphase's, so Go
// requires exact type identity for Enqueue's []GraphProjectionPhaseRepair
// parameter: gpphase's concrete repair queue implementation cannot satisfy
// code/semantic's interface directly even though every field matches. This
// adapter is the narrow translation the two named types need.
type semanticEntityRepairQueueAdapter struct {
	queue gpphase.PhaseRepairQueue
}

// Enqueue converts code/semantic's local repair rows to gpphase's shape and
// forwards to the wrapped queue.
func (a semanticEntityRepairQueueAdapter) Enqueue(ctx context.Context, repairs []semantic.GraphProjectionPhaseRepair) error {
	converted := make([]gpphase.PhaseRepair, len(repairs))
	for i, row := range repairs {
		converted[i] = gpphase.PhaseRepair{
			Key:           row.Key,
			Phase:         row.Phase,
			CommittedAt:   row.CommittedAt,
			EnqueuedAt:    row.EnqueuedAt,
			NextAttemptAt: row.NextAttemptAt,
			UpdatedAt:     row.UpdatedAt,
			Attempts:      row.Attempts,
			LastError:     row.LastError,
		}
	}
	return a.queue.Enqueue(ctx, converted)
}
