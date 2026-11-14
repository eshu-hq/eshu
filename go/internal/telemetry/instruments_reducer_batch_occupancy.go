// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionWorker labels per-worker instruments with the numeric worker
// index ("0", "1", ...). The value space is the configured pool size
// (ESHU_REDUCER_WORKERS, 8 in the #10480 report), a closed set chosen by the
// operator like collector_instance_id — never an intent, scope, or
// generation id.
const MetricDimensionWorker = "worker"

// AttrWorker returns a worker attribute. v must be a pool index within the
// configured worker count.
func AttrWorker(v int) attribute.KeyValue {
	return attribute.String(MetricDimensionWorker, strconv.Itoa(v))
}

// BatchWorkerOccupancy is one reducer batch-claim worker's current execution
// state as seen by the occupancy tracker. Busy is false while the worker is
// idle between handoffs; Domain and IntentID name the in-flight execution,
// and ExecutionAgeSeconds is zero while idle. IntentID never reaches a metric
// label: it is unbounded operator data, so it belongs on spans and logs only,
// matching the fairness_key discipline.
type BatchWorkerOccupancy struct {
	// Worker is the pool index (0-based, bounded by the configured count).
	Worker int
	// Busy reports whether the worker is inside an execution.
	Busy bool
	// Domain is the in-flight intent's domain (closed reducer domain set).
	Domain string
	// IntentID is the in-flight intent's id, for logs and spans only.
	IntentID string
	// ExecutionAgeSeconds is the in-flight execution's age, zero while idle.
	ExecutionAgeSeconds float64
}

// BatchWorkerOccupancyObserver provides per-worker execution state for the
// reducer batch-claim occupancy gauge (#10480).
type BatchWorkerOccupancyObserver interface {
	// BatchWorkerOccupancy returns the current per-worker execution state.
	// Implementations must never key or label by raw intent, scope, or
	// generation identifiers beyond the struct fields; the gauge observes
	// the closed worker and domain labels only.
	BatchWorkerOccupancy(ctx context.Context) ([]BatchWorkerOccupancy, error)
}

// RegisterReducerBatchWorkerOccupancyGauge registers the
// eshu_dp_reducer_batch_worker_execution_age_seconds gauge backed by obs. One
// callback reads the snapshot once per scrape and observes the gauge once per
// busy worker (the poison-liveness idiom), keeping the series on one refresh
// generation. Only busy workers are observed: an absent worker series means
// idle, and the value is the execution age in seconds. Labels are the closed
// queue, worker, and domain sets — no intent id, which is unbounded. A nil
// observer registers nothing and reports success, so lanes without a batch
// pool keep today's instruments byte-identical.
func RegisterReducerBatchWorkerOccupancyGauge(inst *Instruments, meter metric.Meter, obs BatchWorkerOccupancyObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if obs == nil {
		return nil
	}

	var err error
	inst.ReducerBatchWorkerExecutionAge, err = meter.Float64ObservableGauge(
		"eshu_dp_reducer_batch_worker_execution_age_seconds",
		metric.WithDescription("Age in seconds of each busy reducer batch-claim worker's current execution, by worker index and domain; absent while the worker is idle"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register ReducerBatchWorkerExecutionAge gauge: %w", err)
	}
	if _, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		snapshot, err := obs.BatchWorkerOccupancy(ctx)
		if err != nil {
			return err
		}
		for _, worker := range snapshot {
			if !worker.Busy {
				continue
			}
			age := worker.ExecutionAgeSeconds
			if age < 0 {
				age = 0
			}
			o.ObserveFloat64(inst.ReducerBatchWorkerExecutionAge, age,
				metric.WithAttributes(
					attribute.String("queue", "reducer"),
					AttrWorker(worker.Worker),
					AttrDomain(worker.Domain),
				),
			)
		}
		return nil
	}, inst.ReducerBatchWorkerExecutionAge); err != nil {
		return fmt.Errorf("register ReducerBatchWorkerExecutionAge callback: %w", err)
	}
	return nil
}

// registerReducerBatchWorkerOccupancy appends the worker dimension key used
// by the #10480 batch-claim occupancy gauge.
func registerReducerBatchWorkerOccupancy() {
	metricDimensionKeys = append(metricDimensionKeys, MetricDimensionWorker)
}
