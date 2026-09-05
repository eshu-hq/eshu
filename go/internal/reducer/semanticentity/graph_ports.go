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
// The field set matches root's struct field for field. The text is not
// identical: Key and Phase are spelled gpphase.PhaseKey and gpphase.Phase here
// where root spells them GraphProjectionPhaseKey and GraphProjectionPhase --
// the same types through root's aliases.
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
// and narrowed to the one method this package calls: Enqueue. But because
// Enqueue's parameter names GraphProjectionPhaseRepair — a struct, not an
// interface — Go requires exact type identity between this package's
// GraphProjectionPhaseRepair and the root's, even though every field
// matches. So the root's concrete repair queue cannot satisfy this
// declaration directly; defaults_domain_catalog.go wires it in through
// semanticEntityRepairQueueAdapter
// (internal/reducer/semantic_entity_repair_queue_adapter.go), a narrow
// translation between the two named types. This declaration still earns its
// place: the package must not import the reducer root (issue #6061), so it
// cannot reference the root's wider GraphProjectionPhaseRepairQueue (which
// also needs ListDue/Delete/MarkFailed for the root's repair runner) at all.
type GraphProjectionPhaseRepairQueue interface {
	Enqueue(context.Context, []GraphProjectionPhaseRepair) error
}

// graphProjectionPhaseRepairsFromStates converts exact readiness publications
// into durable repair rows that can be retried later if publication failed.
// The body is a byte-for-byte copy of root's
// GraphProjectionPhaseRepairsFromStates (graph_projection_phase_repair.go),
// which this package cannot import for the same reason as above. The
// signature differs: the name is unexported here, and the parameter is spelled
// []gpphase.PhaseState rather than []GraphProjectionPhaseState -- the same
// type through the root's alias.
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
