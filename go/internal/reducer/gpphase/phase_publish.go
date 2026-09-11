// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// PublishIntentGraphPhase publishes the readiness milestone for one durable
// reducer intent (issue #6061; moved here from the reducer root's
// publishIntentGraphPhase). A nil publisher and an intent that cannot name a
// bounded slice (see [StateForIntentValue]) are both no-ops, so a handler
// wired without readiness publication still runs.
//
// This is the one exception to the rest of this package's plain-data, pure-
// builder contract: it performs the publish I/O through the [PhasePublisher]
// interface. It moved here, rather than staying at the root or following
// platformfam's per-family local-wrapper pattern (see the package doc for
// why that pattern existed and why this supersedes it for the
// reducer-root-owned publish path), because the same four hoisted symbols
// (this, [StateForIntentValue], [EndpointPresenceWriter],
// [EndpointPresenceRow]) are the last root-owned pieces blocking the ec2,
// s3, iam, and security_group families from splitting out of the reducer
// root without importing it.
func PublishIntentGraphPhase(
	ctx context.Context,
	publisher PhasePublisher,
	intent reducercontract.Intent,
	keyspace Keyspace,
	phase Phase,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	state, ok := StateForIntentValue(intent, keyspace, phase, observedAt)
	if !ok {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, []PhaseState{state}); err != nil {
		return fmt.Errorf("publish %s phase: %w", phase, err)
	}
	return nil
}

// PublishIntentGraphPhaseWithRepair is [PublishIntentGraphPhase] with retry:
// a publish failure enqueues the state onto repairQueue (when non-nil) so a
// repair runner can retry it later, rather than losing the readiness
// publication when the underlying graph write already committed
// successfully (issue #6061; moved here from the reducer root's
// publishIntentGraphPhaseWithRepair). A nil publisher, or an intent that
// cannot name a bounded slice, is a no-op exactly as in
// [PublishIntentGraphPhase].
func PublishIntentGraphPhaseWithRepair(
	ctx context.Context,
	publisher PhasePublisher,
	repairQueue PhaseRepairQueue,
	intent reducercontract.Intent,
	keyspace Keyspace,
	phase Phase,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	state, ok := StateForIntentValue(intent, keyspace, phase, observedAt)
	if !ok {
		return nil
	}
	if err := PublishPhaseStatesWithRepair(ctx, publisher, repairQueue, []PhaseState{state}); err != nil {
		return fmt.Errorf("publish %s phase: %w", phase, err)
	}
	return nil
}

// PublishPhaseStatesWithRepair publishes a batch of readiness states and, on
// failure, enqueues them onto repairQueue (when non-nil) rather than losing
// the publication (issue #6061; moved here from the reducer root's
// publishGraphProjectionPhaseStatesWithRepair). Do not skip the enqueue when
// a publish fails: the phase publication and the underlying graph write are
// not atomic, so a failed publish after a committed write needs the repair
// queue to retry it.
func PublishPhaseStatesWithRepair(
	ctx context.Context,
	publisher PhasePublisher,
	repairQueue PhaseRepairQueue,
	states []PhaseState,
) error {
	if publisher == nil || len(states) == 0 {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, states); err != nil {
		if repairQueue != nil {
			repairs := PhaseRepairsFromStates(states, err.Error(), time.Now().UTC())
			if enqueueErr := repairQueue.Enqueue(ctx, repairs); enqueueErr != nil {
				return fmt.Errorf("%w (enqueue repairs: %v)", err, enqueueErr)
			}
		}
		return err
	}
	return nil
}
