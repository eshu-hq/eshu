// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudasset

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// recordingGraphProjectionPhasePublisher is a local copy of the reducer
// root's shared test fake (graph_projection_phase_repair_test.go). Go test
// files cannot share unexported symbols across a package boundary, so this
// family keeps its own scoped copy rather than exporting root test
// scaffolding, mirroring the ec2blockkms precedent (issue #6061).
type recordingGraphProjectionPhasePublisher struct {
	calls [][]gpphase.PhaseState
	err   error
}

func (r *recordingGraphProjectionPhasePublisher) PublishGraphProjectionPhases(_ context.Context, rows []gpphase.PhaseState) error {
	cloned := make([]gpphase.PhaseState, len(rows))
	copy(cloned, rows)
	r.calls = append(r.calls, cloned)
	return r.err
}
