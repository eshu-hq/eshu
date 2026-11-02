// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// OwnedTargetSlotObservation is what one progress-ordered owned-package
// planning pass reports for one collector instance (#9776): the outcomes the
// pass went through (a closed set, see the workflow.SlotSelection constants)
// and the oldest planned slot age. AgeKnown is false until every virtual slot
// has been planned once, because before that the oldest row is not a rotation
// age; the recorder then drops the instance's age series.
type OwnedTargetSlotObservation struct {
	CollectorInstanceID string
	Outcomes            []string
	OldestSlotAge       time.Duration
	AgeKnown            bool
}

// ownedTargetSlotInstruments serves the #9776 slot signals: the oldest planned
// slot age gauge and the selection outcome counter. The last age per collector
// instance sits behind a mutex because the reconcile loop writes it and the
// metrics reader's callback reads it from another goroutine; the map is bounded
// by the number of vulnerability-intelligence instances.
type ownedTargetSlotInstruments struct {
	selectionTotal metric.Int64Counter

	mu    sync.Mutex
	ageBy map[string]time.Duration
}

// newOwnedTargetSlotInstruments registers the slot age gauge and the selection
// counter. The full literal names are deliberate: verify-telemetry-coverage.sh
// only recognizes a metric whose constructor's first argument is a literal.
func newOwnedTargetSlotInstruments(meter metric.Meter) (*ownedTargetSlotInstruments, error) {
	instruments := &ownedTargetSlotInstruments{ageBy: map[string]time.Duration{}}
	age, err := meter.Float64ObservableGauge(
		"eshu_dp_owned_target_oldest_slot_age_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Age of the least recently planned owned-package virtual slot at the latest read, per collector instance (#9776)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register owned target oldest slot age gauge: %w", err)
	}
	selection, err := meter.Int64Counter(
		"eshu_dp_owned_target_slot_selection_total",
		metric.WithDescription("Owned-package slot selection outcomes per planning pass, by collector instance and outcome (#9776)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register owned target slot selection counter: %w", err)
	}
	instruments.selectionTotal = selection
	if _, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		for instanceID, value := range instruments.snapshot() {
			observer.ObserveFloat64(age, value.Seconds(),
				metric.WithAttributes(attribute.String(telemetry.MetricDimensionCollectorInstanceID, instanceID)))
		}
		return nil
	}, age); err != nil {
		return nil, fmt.Errorf("register owned target slot age callback: %w", err)
	}
	return instruments, nil
}

func (i *ownedTargetSlotInstruments) snapshot() map[string]time.Duration {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make(map[string]time.Duration, len(i.ageBy))
	for instanceID, value := range i.ageBy {
		out[instanceID] = value
	}
	return out
}

// RecordOwnedTargetSlotSelection counts the pass's outcomes and stores the
// instance's oldest slot age for the gauge (#9776). Outcomes are the closed
// workflow.SlotSelection set, so the label stays low-cardinality.
func (m *otelMetrics) RecordOwnedTargetSlotSelection(ctx context.Context, observation OwnedTargetSlotObservation) {
	if m == nil || m.ownedSlots == nil {
		return
	}
	for _, outcome := range observation.Outcomes {
		m.ownedSlots.selectionTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionCollectorInstanceID, observation.CollectorInstanceID),
			attribute.String(telemetry.MetricDimensionOutcome, outcome),
		))
	}
	m.ownedSlots.mu.Lock()
	defer m.ownedSlots.mu.Unlock()
	if !observation.AgeKnown {
		delete(m.ownedSlots.ageBy, observation.CollectorInstanceID)
		return
	}
	m.ownedSlots.ageBy[observation.CollectorInstanceID] = observation.OldestSlotAge
}
