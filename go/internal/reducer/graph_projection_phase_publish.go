// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// publishIntentGraphPhase publishes the readiness milestone for one intent.
// It forwards to [gpphase.PublishIntentGraphPhase] (issue #6061, moved from
// this file's own former body) so every existing call site in this package
// keeps working unchanged, the same way the type aliases in
// graph_projection_phase.go do.
func publishIntentGraphPhase(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	intent Intent,
	keyspace GraphProjectionKeyspace,
	phase GraphProjectionPhase,
	observedAt time.Time,
) error {
	return gpphase.PublishIntentGraphPhase(ctx, publisher, intent, keyspace, phase, observedAt)
}

func publishIntentGraphPhaseWithRepair(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	repairQueue GraphProjectionPhaseRepairQueue,
	intent Intent,
	keyspace GraphProjectionKeyspace,
	phase GraphProjectionPhase,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	state, ok := graphProjectionPhaseStateForIntent(intent, keyspace, phase, observedAt)
	if !ok {
		return nil
	}
	if err := publishGraphProjectionPhaseStatesWithRepair(ctx, publisher, repairQueue, []GraphProjectionPhaseState{state}); err != nil {
		return fmt.Errorf("publish %s phase: %w", phase, err)
	}
	return nil
}

func publishGraphProjectionPhaseStatesWithRepair(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	repairQueue GraphProjectionPhaseRepairQueue,
	states []GraphProjectionPhaseState,
) error {
	if publisher == nil || len(states) == 0 {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, states); err != nil {
		if repairQueue != nil {
			repairs := GraphProjectionPhaseRepairsFromStates(states, err.Error(), time.Now().UTC())
			if enqueueErr := repairQueue.Enqueue(ctx, repairs); enqueueErr != nil {
				return fmt.Errorf("%w (enqueue repairs: %v)", err, enqueueErr)
			}
		}
		return err
	}
	return nil
}

// graphProjectionPhaseStateForIntent builds one durable readiness publication
// for the given intent, keyspace, and phase. It forwards to
// [gpphase.StateForIntentValue] (issue #6061, moved from this file's own
// former body) so every existing call site in this package keeps working
// unchanged. A family that only needs the key to read readiness (not to
// publish a state) can call [gpphase.KeyFromScope] directly instead of
// importing the root.
func graphProjectionPhaseStateForIntent(
	intent Intent,
	keyspace GraphProjectionKeyspace,
	phase GraphProjectionPhase,
	observedAt time.Time,
) (GraphProjectionPhaseState, bool) {
	return gpphase.StateForIntentValue(intent, keyspace, phase, observedAt)
}
