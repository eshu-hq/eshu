// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RequeueReasonObserver is the optional queue-observer surface for the
// #11615 per-reason queue signals. RequeueReasonDepths reports live queue
// depth by stage, status, and requeue reason (NULL reads as
// legacy_unclassified); RequeueReasonOldestAge reports the oldest
// outstanding-item age in seconds by requeue reason. The store implements it
// over the same active-rows CTE that powers queue depth and age, so the
// reason breakdown costs no new scan beyond the existing poll's GROUP BY.
type RequeueReasonObserver interface {
	RequeueReasonDepths(ctx context.Context) (map[string]map[string]map[string]int64, error)
	RequeueReasonOldestAge(ctx context.Context) (map[string]float64, error)
}

// registerReducerRequeueInstruments registers the #11615 requeue counter on
// inst: one add per actually-transitioned row, labeled by closed-vocabulary
// reason only (never domain, scope, or work-item id).
func registerReducerRequeueInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	inst.ReducerRequeueTotal, err = meter.Int64Counter(
		"eshu_dp_reducer_requeue_total",
		metric.WithDescription("Reducer queue rows returned to an outstanding state, by closed-vocabulary requeue reason (#11615). Every requeue writer emits its actually-transitioned row count; claimed or running rows newly flagged for replay-debt count at schedule time, and the database ACK-trigger conversion they later cause is never double-counted."),
	)
	if err != nil {
		return fmt.Errorf("register ReducerRequeueTotal counter: %w", err)
	}
	return nil
}

// registerRequeueReasonGauges registers the live per-reason depth and age
// gauges on the same poll that already powers queue depth and age. The
// legacy_unclassified series is the rollout-ramp proof: it must trend to ~0
// for reducer rows as the fleet rolls, and any plateau is a missed writer.
func registerRequeueReasonGauges(inst *Instruments, meter metric.Meter, obs RequeueReasonObserver) error {
	var err error
	inst.RequeueReasonDepth, err = meter.Int64ObservableGauge(
		"eshu_dp_reducer_requeue_depth",
		metric.WithDescription("Live reducer queue depth by stage, status, and requeue reason; NULL reasons read as legacy_unclassified (#11615)"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			depths, err := obs.RequeueReasonDepths(ctx)
			if err != nil {
				return fmt.Errorf("requeue reason depths: %w", err)
			}
			for stage, statuses := range depths {
				for status, reasons := range statuses {
					for reason, count := range reasons {
						o.Observe(
							count,
							metric.WithAttributes(
								attribute.String("queue", stage),
								attribute.String(MetricDimensionStatus, status),
								attribute.String(MetricDimensionReason, reason),
							),
						)
					}
				}
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register RequeueReasonDepth gauge: %w", err)
	}
	inst.RequeueReasonOldestAge, err = meter.Float64ObservableGauge(
		"eshu_dp_reducer_requeue_oldest_age_seconds",
		metric.WithDescription("Age in seconds of the oldest outstanding reducer queue item by requeue reason; NULL reasons read as legacy_unclassified (#11615)"),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(ctx context.Context, o metric.Float64Observer) error {
			ages, err := obs.RequeueReasonOldestAge(ctx)
			if err != nil {
				return fmt.Errorf("requeue reason oldest age: %w", err)
			}
			for reason, age := range ages {
				o.Observe(
					age,
					metric.WithAttributes(
						attribute.String(MetricDimensionReason, reason),
					),
				)
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register RequeueReasonOldestAge gauge: %w", err)
	}
	return nil
}

// RecordReducerRequeue counts actually-transitioned requeue rows under one
// closed-vocabulary reason. Nil instruments or an unregistered counter are a
// safe no-op so existing construction sites keep working. Callers in a
// caller-owned transaction must accumulate and emit after commit; a
// rolled-back transaction must not count. Count the rows that transitioned
// (RowsAffected where matched equals transitioned, RETURNING-derived counts
// for the CASE writers), never the rows merely matched.
func RecordReducerRequeue(ctx context.Context, inst *Instruments, reason RequeueReason, count int64) {
	if inst == nil || inst.ReducerRequeueTotal == nil {
		return
	}
	if count <= 0 {
		return
	}
	inst.ReducerRequeueTotal.Add(ctx, count, metric.WithAttributes(AttrRequeueReason(reason)))
}
