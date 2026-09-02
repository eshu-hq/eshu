// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

func publishIntentGraphPhase(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
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
	if err := publisher.PublishGraphProjectionPhases(ctx, []GraphProjectionPhaseState{state}); err != nil {
		return fmt.Errorf("publish %s phase: %w", phase, err)
	}
	return nil
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
// for the given intent, keyspace, and phase. The key is [gpphase.KeyFromScope]
// (issue #6061); a family that only needs the key to read readiness (not to
// publish a state) can call that directly instead of importing the root.
func graphProjectionPhaseStateForIntent(
	intent Intent,
	keyspace GraphProjectionKeyspace,
	phase GraphProjectionPhase,
	observedAt time.Time,
) (GraphProjectionPhaseState, bool) {
	key, ok := gpphase.KeyFromScope(intent.ScopeID, intent.GenerationID, intent.EntityKeys, keyspace)
	if !ok {
		return GraphProjectionPhaseState{}, false
	}

	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	observedAt = observedAt.UTC()

	return GraphProjectionPhaseState{
		Key:         key,
		Phase:       phase,
		CommittedAt: observedAt,
		UpdatedAt:   observedAt,
	}, true
}
