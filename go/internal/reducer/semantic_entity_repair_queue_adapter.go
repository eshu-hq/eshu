// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
)

// semanticEntityRepairQueueAdapter adapts the reducer root's
// GraphProjectionPhaseRepairQueue to semantic.GraphProjectionPhaseRepairQueue.
//
// The two repair structs are field-for-field identical, but code/semantic
// declares its own GraphProjectionPhaseRepair struct locally (issue #6061,
// see code/semantic/graph_ports.go) rather than importing the root's, so Go
// requires exact type identity for Enqueue's []GraphProjectionPhaseRepair
// parameter: the root's concrete repair queue implementation cannot satisfy
// code/semantic's interface directly even though every field matches. This
// adapter is the narrow translation the two named types need.
type semanticEntityRepairQueueAdapter struct {
	queue GraphProjectionPhaseRepairQueue
}

// Enqueue converts code/semantic's local repair rows to the root's shape and
// forwards to the wrapped queue.
func (a semanticEntityRepairQueueAdapter) Enqueue(ctx context.Context, repairs []semantic.GraphProjectionPhaseRepair) error {
	converted := make([]GraphProjectionPhaseRepair, len(repairs))
	for i, repair := range repairs {
		converted[i] = GraphProjectionPhaseRepair{
			Key:           repair.Key,
			Phase:         repair.Phase,
			CommittedAt:   repair.CommittedAt,
			EnqueuedAt:    repair.EnqueuedAt,
			NextAttemptAt: repair.NextAttemptAt,
			UpdatedAt:     repair.UpdatedAt,
			Attempts:      repair.Attempts,
			LastError:     repair.LastError,
		}
	}
	return a.queue.Enqueue(ctx, converted)
}
