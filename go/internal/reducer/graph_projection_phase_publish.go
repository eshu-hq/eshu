// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// publishIntentGraphPhase publishes the readiness milestone for one intent.
// It forwards to [gpphase.PublishIntentGraphPhase] (issue #6061, moved from
// this file's own former body) so every existing call site in this package
// keeps working unchanged, the same way the type aliases in
// graph_projection_phase.go do.
func publishIntentGraphPhase(
	ctx context.Context,
	publisher gpphase.PhasePublisher,
	intent reducercontract.Intent,
	keyspace gpphase.Keyspace,
	phase gpphase.Phase,
	observedAt time.Time,
) error {
	return gpphase.PublishIntentGraphPhase(ctx, publisher, intent, keyspace, phase, observedAt)
}

// publishIntentGraphPhaseWithRepair forwards to
// [gpphase.PublishIntentGraphPhaseWithRepair].
func publishIntentGraphPhaseWithRepair(
	ctx context.Context,
	publisher gpphase.PhasePublisher,
	repairQueue GraphProjectionPhaseRepairQueue,
	intent reducercontract.Intent,
	keyspace gpphase.Keyspace,
	phase gpphase.Phase,
	observedAt time.Time,
) error {
	return gpphase.PublishIntentGraphPhaseWithRepair(ctx, publisher, repairQueue, intent, keyspace, phase, observedAt)
}

// publishGraphProjectionPhaseStatesWithRepair forwards to
// [gpphase.PublishPhaseStatesWithRepair].
func publishGraphProjectionPhaseStatesWithRepair(
	ctx context.Context,
	publisher gpphase.PhasePublisher,
	repairQueue GraphProjectionPhaseRepairQueue,
	states []gpphase.PhaseState,
) error {
	return gpphase.PublishPhaseStatesWithRepair(ctx, publisher, repairQueue, states)
}

// graphProjectionPhaseStateForIntent builds one durable readiness publication
// for the given intent, keyspace, and phase. It forwards to
// [gpphase.StateForIntentValue] (issue #6061, moved from this file's own
// former body) so every existing call site in this package keeps working
// unchanged. A family that only needs the key to read readiness (not to
// publish a state) can call [gpphase.KeyFromScope] directly instead of
// importing the root.
func graphProjectionPhaseStateForIntent(
	intent reducercontract.Intent,
	keyspace gpphase.Keyspace,
	phase gpphase.Phase,
	observedAt time.Time,
) (gpphase.PhaseState, bool) {
	return gpphase.StateForIntentValue(intent, keyspace, phase, observedAt)
}
