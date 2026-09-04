// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticentity

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// GraphProjectionPhaseRepair captures one exact readiness publication that
// must be retried after the underlying graph write already committed
// successfully.
//
// Declared locally rather than imported from the reducer root: the root's
// GraphProjectionPhaseRepair (graph_projection_phase_repair.go) is shared
// logic used by several families that have not moved out of root yet
// (workload_materialization_handler.go, graph_projection_phase_repair_runner.go,
// workload_materialization_repo_phase.go), so importing it would violate the
// rule that a family subpackage never imports the reducer root (issue #6061).
// The field set is copied byte-for-byte from root's struct.
type GraphProjectionPhaseRepair struct {
	Key           gpphase.PhaseKey
	Phase         gpphase.Phase
	CommittedAt   time.Time
	EnqueuedAt    time.Time
	NextAttemptAt time.Time
	UpdatedAt     time.Time
	Attempts      int
	LastError     string
}

// GraphProjectionPhaseRepairQueue persists exact readiness publications that
// must be retried later after a publish failure.
//
// Declared locally for the same reason as GraphProjectionPhaseRepair above,
// and narrowed to the one method this package calls: Go interfaces are
// satisfied structurally, so the same concrete repair queue cmd/reducer wires
// into the root's wider GraphProjectionPhaseRepairQueue (which also needs
// ListDue/Delete/MarkFailed for the root's repair runner) also satisfies this
// narrower local declaration without any code duplication.
type GraphProjectionPhaseRepairQueue interface {
	Enqueue(context.Context, []GraphProjectionPhaseRepair) error
}

// graphProjectionPhaseRepairsFromStates converts exact readiness publications
// into durable repair rows that can be retried later if publication failed.
// Copied byte-for-byte from root's GraphProjectionPhaseRepairsFromStates
// (graph_projection_phase_repair.go), which this package cannot import for
// the same reason as above.
func graphProjectionPhaseRepairsFromStates(
	states []gpphase.PhaseState,
	lastError string,
	enqueuedAt time.Time,
) []GraphProjectionPhaseRepair {
	repairs := make([]GraphProjectionPhaseRepair, 0, len(states))
	queuedAt := enqueuedAt.UTC()
	if queuedAt.IsZero() {
		queuedAt = time.Now().UTC()
	}

	for _, state := range states {
		repairs = append(repairs, GraphProjectionPhaseRepair{
			Key:           state.Key,
			Phase:         state.Phase,
			CommittedAt:   state.CommittedAt,
			EnqueuedAt:    queuedAt,
			NextAttemptAt: queuedAt,
			UpdatedAt:     queuedAt,
			LastError:     lastError,
		})
	}
	return repairs
}
