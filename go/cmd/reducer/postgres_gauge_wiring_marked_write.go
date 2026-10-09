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
// marked-write age gauge. It embeds the incoming cached observer as the
// telemetry.QueueObserver interface rather than one concrete wrapper, so one
// type covers every upstream shape (plain, source-queue, claim-invariant, and
// their combinations); the ProjectorMarkedWriteObserver assertion in
// RegisterObservableGauges keeps its exact meaning because only this wrapper
// provides the method.
type cachedMarkedWriteQueueObserver struct {
	telemetry.QueueObserver
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
	return cachedMarkedWriteQueueObserver{
		QueueObserver:                      cached,
		cachedProjectorMarkedWriteObserver: cachedProjectorMarkedWriteObserver{source: source},
	}, nil
}
