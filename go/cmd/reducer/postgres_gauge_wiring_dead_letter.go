// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
	"go.opentelemetry.io/otel/metric"
)

const (
	// gaugeSupplyChainImpactDeadLetters is the `gauge` label of the snapshot
	// that feeds the #11147 active-generation dead-letter gauges.
	gaugeSupplyChainImpactDeadLetters = "supply_chain_impact_dead_letters"

	// The snapshot keys are "<series>/<failure_class>", because a snapshot is
	// a flat map of counts.
	deadLetterTotalKeyPrefix = "total/"
	deadLetterOwingKeyPrefix = "owing/"
)

// cachedSupplyChainImpactDeadLetterObserver serves the #11147 gauges from one
// snapshot. One refresh runs the dead-letter count statement, so the scrape
// never queries Postgres and the statement's cost is bounded by the refresh
// cadence.
type cachedSupplyChainImpactDeadLetterObserver struct{ source *snapshot.Source }

func (c cachedSupplyChainImpactDeadLetterObserver) SupplyChainImpactActiveDeadLetters(ctx context.Context) ([]telemetry.SupplyChainImpactDeadLetterCount, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return nil, err
	}
	if counts == nil {
		// No reading yet: observing nothing beats a false "no dead letters".
		return nil, errPostgresGaugeSnapshotNotReady
	}
	byClass := map[string]*telemetry.SupplyChainImpactDeadLetterCount{}
	entry := func(class string) *telemetry.SupplyChainImpactDeadLetterCount {
		if byClass[class] == nil {
			byClass[class] = &telemetry.SupplyChainImpactDeadLetterCount{FailureClass: class}
		}
		return byClass[class]
	}
	for key, value := range counts {
		switch {
		case strings.HasPrefix(key, deadLetterTotalKeyPrefix):
			entry(strings.TrimPrefix(key, deadLetterTotalKeyPrefix)).Total = value
		case strings.HasPrefix(key, deadLetterOwingKeyPrefix):
			entry(strings.TrimPrefix(key, deadLetterOwingKeyPrefix)).OwingFanOut = value
		}
	}
	out := make([]telemetry.SupplyChainImpactDeadLetterCount, 0, len(byClass))
	for _, count := range byClass {
		out = append(out, *count)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FailureClass < out[j].FailureClass })
	return out, nil
}

var _ telemetry.SupplyChainImpactDeadLetterObserver = cachedSupplyChainImpactDeadLetterObserver{}

// registerSupplyChainImpactDeadLetterGauges registers the #11147 snapshot
// source fed by obs and serves the dead-letter gauges from it. A failed
// refresh publishes nothing, so the previous snapshot ages instead of a false
// zero appearing.
func registerSupplyChainImpactDeadLetterGauges(
	refresher *snapshot.Refresher,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	obs telemetry.SupplyChainImpactDeadLetterObserver,
) error {
	source, err := refresher.Register(gaugeSupplyChainImpactDeadLetters, func(ctx context.Context) (map[string]int64, error) {
		counts, err := obs.SupplyChainImpactActiveDeadLetters(ctx)
		if err != nil {
			return nil, err
		}
		out := make(map[string]int64, 2*len(counts))
		for _, count := range counts {
			out[deadLetterTotalKeyPrefix+count.FailureClass] = count.Total
			out[deadLetterOwingKeyPrefix+count.FailureClass] = count.OwingFanOut
		}
		return out, nil
	})
	if err != nil {
		return fmt.Errorf("register supply chain impact dead-letter snapshot source: %w", err)
	}
	if err := telemetry.RegisterSupplyChainImpactDeadLetterObservableGauges(instruments, meter, cachedSupplyChainImpactDeadLetterObserver{source: source}); err != nil {
		return fmt.Errorf("register supply chain impact dead-letter observable gauges: %w", err)
	}
	return nil
}
