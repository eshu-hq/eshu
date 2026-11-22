// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Reducer admission depth signals (#11233), recorded on the operation
// dimension of eshu_dp_reducer_admission_depth_reads_total. The value set is
// closed: one per depth query the ingester admission gate consults.
const (
	// ReducerAdmissionDepthSignalQueue is the reducer total of the queue
	// observer's QueueDepths, the high-water signal.
	ReducerAdmissionDepthSignalQueue = "queue_depth"
	// ReducerAdmissionDepthSignalGraphWriteTimeout is the retrying
	// graph_write_timeout depth, the graph-write-pressure signal.
	ReducerAdmissionDepthSignalGraphWriteTimeout = "graph_write_timeout_depth"
)

// Reducer admission depth read outcomes (#11233), recorded on the outcome
// dimension. The value set is closed.
const (
	// ReducerAdmissionDepthReadRead means this decision issued the read and
	// it succeeded.
	ReducerAdmissionDepthReadRead = "read"
	// ReducerAdmissionDepthReadShared means this decision joined a read
	// another decision had in flight and it succeeded.
	ReducerAdmissionDepthReadShared = "shared"
	// ReducerAdmissionDepthReadHeld means this decision used the held depth
	// because it was inside the window; no SQL ran.
	ReducerAdmissionDepthReadHeld = "held"
	// ReducerAdmissionDepthReadError means the read this decision issued or
	// joined failed; the error reached the caller and nothing was held.
	ReducerAdmissionDepthReadError = "error"
	// ReducerAdmissionDepthReadCancelled means the caller's context ended
	// while it waited; the shared read went on for any other waiter.
	ReducerAdmissionDepthReadCancelled = "cancelled"
)

// registerReducerAdmissionDepthInstruments registers the #11233 shared
// queue-depth read instruments of the ingester reducer admission gate.
//
// eshu_dp_reducer_admission_depth_reads_total{operation,outcome}: one
// increment per signal per admission decision. The SQL rate is the read plus
// error outcomes; held and shared are decisions served without a new
// statement.
//
// eshu_dp_reducer_admission_depth_age_seconds: the age of the oldest depth a
// decision used, from the start of its read. Admitting decisions stay at or
// under one poll interval; deferring decisions can reach the backoff.
//
// eshu_dp_reducer_admission_read_backoff_seconds: the current read interval
// while the gate defers; 0 while it admits. It grows from the poll interval
// to the cap (10 s, or half the max wait when smaller) and resets on the
// first admit.
func registerReducerAdmissionDepthInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReducerAdmissionDepthReads, err = meter.Int64Counter(
		"eshu_dp_reducer_admission_depth_reads_total",
		metric.WithDescription("Ingester reducer admission depth lookups per decision by operation (queue_depth, graph_write_timeout_depth) and outcome (read, shared, held, error, cancelled); only read and error ran SQL (#11233)"),
	); err != nil {
		return fmt.Errorf("register ReducerAdmissionDepthReads counter: %w", err)
	}
	if inst.ReducerAdmissionDepthAge, err = meter.Float64Histogram(
		"eshu_dp_reducer_admission_depth_age_seconds",
		metric.WithDescription("Age of the oldest queue depth an ingester reducer admission decision used, from the start of its read (#11233)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30),
	); err != nil {
		return fmt.Errorf("register ReducerAdmissionDepthAge histogram: %w", err)
	}
	if inst.ReducerAdmissionReadBackoff, err = meter.Float64Gauge(
		"eshu_dp_reducer_admission_read_backoff_seconds",
		metric.WithDescription("Current interval between ingester reducer admission depth reads while the gate defers; 0 while it admits (#11233)"),
		metric.WithUnit("s"),
	); err != nil {
		return fmt.Errorf("register ReducerAdmissionReadBackoff gauge: %w", err)
	}
	return nil
}
