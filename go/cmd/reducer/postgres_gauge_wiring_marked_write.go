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
	// gaugeProjectorMarkedWrite is the `gauge` label of the snapshot that
	// feeds eshu_dp_projector_marked_write_oldest_age_seconds (#7471).
	gaugeProjectorMarkedWrite = "reducer_projector_marked_write"

	projectorMarkedWriteOldestAgeMillisKey = "oldest_age_millis"
)

// cachedProjectorMarkedWriteObserver serves the #7471 marked-write age gauge
// from a snapshot. One refresh runs the age query, so the scrape never
// queries Postgres. The snapshot stores millis; the gauge observes seconds.
type cachedProjectorMarkedWriteObserver struct{ source *snapshot.Source }

func (c cachedProjectorMarkedWriteObserver) ProjectorMarkedWriteOldestAge(ctx context.Context) (float64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return 0, err
	}
	if counts == nil {
		// A zero means no marked write is outstanding, so a pre-success
		// zero would lie.
		return 0, errPostgresGaugeSnapshotNotReady
	}
	return millisToSeconds(counts[projectorMarkedWriteOldestAgeMillisKey]), nil
}

// cachedMarkedWriteQueueObserver is the queue observer that also serves the
// marked-write age gauge. Each combination with the upstream cached shapes
// (plain, source-queue, claim-invariant, claim+source) is its own type
// embedding the concrete wrapper, because only concrete embedding promotes
// the upstream optional methods: embedding the telemetry.QueueObserver
// interface would drop the source-queue and claim-invariant contracts and
// their gauges would silently unregister.
type cachedMarkedWriteQueueObserver struct {
	cachedQueueObserver
	cachedProjectorMarkedWriteObserver
}

// cachedMarkedWriteSourceQueueObserver is cachedMarkedWriteQueueObserver plus
// the source-system queue gauges.
type cachedMarkedWriteSourceQueueObserver struct {
	cachedSourceQueueObserver
	cachedProjectorMarkedWriteObserver
}

// cachedMarkedWriteClaimQueueObserver is cachedMarkedWriteQueueObserver plus
// the projector claim invariant gauges.
type cachedMarkedWriteClaimQueueObserver struct {
	cachedClaimQueueObserver
	cachedProjectorMarkedWriteObserver
}

// cachedMarkedWriteClaimSourceQueueObserver is
// cachedMarkedWriteSourceQueueObserver plus the projector claim invariant
// gauges.
type cachedMarkedWriteClaimSourceQueueObserver struct {
	cachedClaimSourceQueueObserver
	cachedProjectorMarkedWriteObserver
}

var _ telemetry.ProjectorMarkedWriteObserver = cachedMarkedWriteQueueObserver{}

// withProjectorMarkedWriteAge registers the marked-write age snapshot source
// fed by markedObs and returns cached extended so it implements
// telemetry.ProjectorMarkedWriteObserver. A failed read publishes nothing, so
// the previous snapshot ages instead of a false zero appearing.
func withProjectorMarkedWriteAge(
	refresher *snapshot.Refresher,
	cached telemetry.QueueObserver,
	markedObs telemetry.ProjectorMarkedWriteObserver,
) (telemetry.QueueObserver, error) {
	source, err := refresher.Register(gaugeProjectorMarkedWrite, func(ctx context.Context) (map[string]int64, error) {
		age, err := markedObs.ProjectorMarkedWriteOldestAge(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]int64{
			projectorMarkedWriteOldestAgeMillisKey: secondsToMillis(age),
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("register projector marked write snapshot source: %w", err)
	}
	marked := cachedProjectorMarkedWriteObserver{source: source}
	switch base := cached.(type) {
	case cachedClaimSourceQueueObserver:
		return cachedMarkedWriteClaimSourceQueueObserver{cachedClaimSourceQueueObserver: base, cachedProjectorMarkedWriteObserver: marked}, nil
	case cachedClaimQueueObserver:
		return cachedMarkedWriteClaimQueueObserver{cachedClaimQueueObserver: base, cachedProjectorMarkedWriteObserver: marked}, nil
	case cachedSourceQueueObserver:
		return cachedMarkedWriteSourceQueueObserver{cachedSourceQueueObserver: base, cachedProjectorMarkedWriteObserver: marked}, nil
	case cachedQueueObserver:
		return cachedMarkedWriteQueueObserver{cachedQueueObserver: base, cachedProjectorMarkedWriteObserver: marked}, nil
	default:
		return nil, fmt.Errorf("wrap cached queue observer for projector marked write: unexpected type %T", cached)
	}
}
