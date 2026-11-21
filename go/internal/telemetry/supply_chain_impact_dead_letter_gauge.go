// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// The #11147 dead-letter gauges. A supply_chain_impact item that is a dead
// letter at its scope's active generation pins every finding copy that scope
// holds until it is reopened, and one that still owes a fan-out also pins the
// copies of every holder it never told; the stale-copy bound holds only while
// both are zero.
const (
	// MetricSupplyChainImpactActiveDeadLetters counts supply_chain_impact
	// reducer items that are dead letters at their scope's active generation,
	// labeled failure_class.
	MetricSupplyChainImpactActiveDeadLetters = "eshu_dp_supply_chain_impact_active_dead_letters"
	// MetricSupplyChainImpactActiveDeadLettersOwingFanOut counts the subset
	// that still carries fanout_owed or fanout_remaining, labeled
	// failure_class. Above zero for 30 minutes is the alert.
	MetricSupplyChainImpactActiveDeadLettersOwingFanOut = "eshu_dp_supply_chain_impact_active_dead_letters_owing_fanout"
)

// SupplyChainImpactDeadLetterCount is one failure class's active-generation
// dead letters. FailureClass is "unclassified" for a dead letter with no class.
type SupplyChainImpactDeadLetterCount struct {
	FailureClass string
	// Total counts every active-generation dead letter in the class.
	Total int64
	// OwingFanOut counts those that still carry fanout_owed or
	// fanout_remaining.
	OwingFanOut int64
}

// SupplyChainImpactDeadLetterObserver reads the #11147 dead-letter counts.
// The reducer serves it from the Postgres gauge snapshot refreshed in the
// background (cmd/reducer), so a scrape never queries Postgres. It returns an
// error until a reading exists, and the gauges then observe nothing rather
// than a false zero.
type SupplyChainImpactDeadLetterObserver interface {
	SupplyChainImpactActiveDeadLetters(ctx context.Context) ([]SupplyChainImpactDeadLetterCount, error)
}

// RegisterSupplyChainImpactDeadLetterObservableGauges registers the two #11147
// dead-letter gauges backed by obs. One callback reads obs once per collection
// and observes both gauges, so the pair always comes from one reading. A nil
// obs registers nothing.
func RegisterSupplyChainImpactDeadLetterObservableGauges(inst *Instruments, meter metric.Meter, obs SupplyChainImpactDeadLetterObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if obs == nil {
		return nil
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	// The names are literals here, not the constants, so the telemetry
	// coverage verifier sees the registration.
	total, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_active_dead_letters",
		metric.WithDescription("supply_chain_impact reducer items dead-lettered at their scope's active generation, by failure_class; each pins its scope's finding copies until reopened"),
	)
	if err != nil {
		return fmt.Errorf("register SupplyChainImpactActiveDeadLetters gauge: %w", err)
	}
	owing, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_active_dead_letters_owing_fanout",
		metric.WithDescription("active-generation supply_chain_impact dead letters that still owe a holder fan-out (fanout_owed or fanout_remaining), by failure_class; above zero means holders were never told of a committed change"),
	)
	if err != nil {
		return fmt.Errorf("register SupplyChainImpactActiveDeadLettersOwingFanOut gauge: %w", err)
	}
	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		counts, err := obs.SupplyChainImpactActiveDeadLetters(ctx)
		if err != nil {
			return fmt.Errorf("observe supply chain impact dead letters: %w", err)
		}
		for _, count := range counts {
			class := metric.WithAttributes(AttrFailureClass(count.FailureClass))
			o.ObserveInt64(total, count.Total, class)
			o.ObserveInt64(owing, count.OwingFanOut, class)
		}
		return nil
	}, total, owing)
	if err != nil {
		return fmt.Errorf("register supply chain impact dead-letter gauge callback: %w", err)
	}
	return nil
}
