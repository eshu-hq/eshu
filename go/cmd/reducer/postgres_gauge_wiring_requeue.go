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
	gaugeRequeueReasonDepth     = "reducer_requeue_reason_depth"
	gaugeRequeueReasonOldestAge = "reducer_requeue_reason_oldest_age"
)

// cachedRequeueReasonObserver serves the per-reason requeue depth and age
// gauges (#11615) from two snapshots. It embeds the QueueObserver the
// snapshots were built over so the served series stay byte-identical to the
// direct callbacks; the RequeueReasonObserver assertion in
// telemetry.RegisterObservableGauges then registers the reason gauges on the
// same poll that already powers queue depth and age.
type cachedRequeueReasonObserver struct {
	telemetry.QueueObserver
	reasonDepths *snapshot.Source
	reasonAges   *snapshot.Source
}

func (c cachedRequeueReasonObserver) RequeueReasonDepths(ctx context.Context) (map[string]map[string]map[string]int64, error) {
	counts, err := c.reasonDepths.Counts(ctx)
	if err != nil {
		return nil, fmt.Errorf("requeue reason depths snapshot: %w", err)
	}
	return unflattenDepth3(counts), nil
}

func (c cachedRequeueReasonObserver) RequeueReasonOldestAge(ctx context.Context) (map[string]float64, error) {
	counts, err := c.reasonAges.Counts(ctx)
	if err != nil {
		return nil, fmt.Errorf("requeue reason oldest age snapshot: %w", err)
	}
	ages := make(map[string]float64, len(counts))
	for reason, millis := range counts {
		ages[reason] = millisToSeconds(millis)
	}
	return ages, nil
}

// registerRequeueReasonSnapshotGauges serves the per-reason requeue depth
// and age gauges (#11615) from two snapshots when the queue observer
// implements telemetry.RequeueReasonObserver, and wraps the cached observer
// so telemetry.RegisterObservableGauges registers the reason gauges on the
// same poll that already powers queue depth and age. A store without the
// optional surface keeps the existing gauges only.
func registerRequeueReasonSnapshotGauges(refresher *snapshot.Refresher, cached telemetry.QueueObserver, queueObs telemetry.QueueObserver) (telemetry.QueueObserver, error) {
	requeueObs, ok := queueObs.(telemetry.RequeueReasonObserver)
	if !ok {
		return cached, nil
	}
	reasonDepths, err := refresher.Register(gaugeRequeueReasonDepth, func(ctx context.Context) (map[string]int64, error) {
		depths, err := requeueObs.RequeueReasonDepths(ctx)
		if err != nil {
			return nil, err
		}
		return flattenDepth3(depths), nil
	})
	if err != nil {
		return cached, fmt.Errorf("register requeue reason depth snapshot source: %w", err)
	}
	reasonAges, err := refresher.Register(gaugeRequeueReasonOldestAge, func(ctx context.Context) (map[string]int64, error) {
		ages, err := requeueObs.RequeueReasonOldestAge(ctx)
		if err != nil {
			return nil, err
		}
		millis := make(map[string]int64, len(ages))
		for reason, age := range ages {
			millis[reason] = secondsToMillis(age)
		}
		return millis, nil
	})
	if err != nil {
		return cached, fmt.Errorf("register requeue reason oldest age snapshot source: %w", err)
	}
	return cachedRequeueReasonObserver{
		QueueObserver: cached,
		reasonDepths:  reasonDepths,
		reasonAges:    reasonAges,
	}, nil
}
