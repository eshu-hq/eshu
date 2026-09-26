// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
)

const (
	// gaugeProjectorClaimInvariants is the `gauge` label of the snapshot that
	// feeds eshu_dp_projector_scopes_multiple_live_leases and
	// eshu_dp_projector_scopes_missing_claim_fence (#7115).
	gaugeProjectorClaimInvariants = "reducer_projector_claim_invariants"

	projectorMultipleLiveLeasesKey = "multiple_live_leases"
	projectorMissingClaimFenceKey  = "missing_claim_fence"
)

// cachedProjectorClaimObserver serves the #7115 projector claim invariant
// gauges from one snapshot. One refresh runs both counting queries, so the
// scrape never queries Postgres (#7064, #7214).
type cachedProjectorClaimObserver struct{ source *snapshot.Source }

func (c cachedProjectorClaimObserver) count(ctx context.Context, key string) (int64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return 0, err
	}
	if counts == nil {
		// A zero means the invariant holds, so a pre-success zero would lie.
		return 0, errPostgresGaugeSnapshotNotReady
	}
	return counts[key], nil
}

func (c cachedProjectorClaimObserver) ProjectorScopesWithMultipleLiveLeases(ctx context.Context) (int64, error) {
	return c.count(ctx, projectorMultipleLiveLeasesKey)
}

func (c cachedProjectorClaimObserver) ProjectorScopesMissingClaimFence(ctx context.Context) (int64, error) {
	return c.count(ctx, projectorMissingClaimFenceKey)
}

// cachedClaimQueueObserver is the queue observer that also serves the
// projector claim invariants. It is a separate type so the
// ProjectorClaimInvariantObserver assertion in RegisterObservableGauges keeps
// its exact meaning: the gauges register only when the underlying observer
// provides them.
type cachedClaimQueueObserver struct {
	cachedQueueObserver
	cachedProjectorClaimObserver
}

// cachedClaimSourceQueueObserver is cachedClaimQueueObserver plus the
// source-system queue gauges.
type cachedClaimSourceQueueObserver struct {
	cachedSourceQueueObserver
	cachedProjectorClaimObserver
}

// withProjectorClaimInvariants registers the projector claim invariant
// snapshot source fed by claimObs and returns cached extended so it implements
// telemetry.ProjectorClaimInvariantObserver. Both counts are read in one
// refresh; a failure of either publishes nothing, so the previous snapshot
// ages instead of a false zero appearing.
func withProjectorClaimInvariants(
	refresher *snapshot.Refresher,
	cached telemetry.QueueObserver,
	claimObs telemetry.ProjectorClaimInvariantObserver,
) (telemetry.QueueObserver, error) {
	source, err := refresher.Register(gaugeProjectorClaimInvariants, func(ctx context.Context) (map[string]int64, error) {
		multiple, err := claimObs.ProjectorScopesWithMultipleLiveLeases(ctx)
		if err != nil {
			return nil, err
		}
		missing, err := claimObs.ProjectorScopesMissingClaimFence(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]int64{
			projectorMultipleLiveLeasesKey: multiple,
			projectorMissingClaimFenceKey:  missing,
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("register projector claim invariant snapshot source: %w", err)
	}
	claim := cachedProjectorClaimObserver{source: source}
	switch base := cached.(type) {
	case cachedSourceQueueObserver:
		return cachedClaimSourceQueueObserver{cachedSourceQueueObserver: base, cachedProjectorClaimObserver: claim}, nil
	case cachedQueueObserver:
		return cachedClaimQueueObserver{cachedQueueObserver: base, cachedProjectorClaimObserver: claim}, nil
	default:
		return nil, fmt.Errorf("wrap cached queue observer for projector claim invariants: unexpected type %T", cached)
	}
}
