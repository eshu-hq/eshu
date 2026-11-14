// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// batchWorkerSlot is one batch-claim worker's tracked execution state. A
// worker is idle while it waits on the workerReady/workCh rendezvous and busy
// while it is inside executeAndReport. The tracker only observes: occupy and
// release never block, signal, or reorder claim, execution, or ack work.
type batchWorkerSlot struct {
	busy      bool
	domain    reducercontract.Domain
	intentID  string
	startedAt time.Time
}

// batchWorkerOccupancy tracks each batch-claim worker's current intent and
// the start time of its current execution for the #10480 occupancy gauge,
// start log line, and run-span attributes. All methods are safe for
// concurrent use by the worker goroutines; all methods on a nil tracker are
// no-ops, so zero-value Services in existing tests behave exactly as before.
type batchWorkerOccupancy struct {
	mu    sync.Mutex
	slots []batchWorkerSlot
}

// newBatchWorkerOccupancy sizes the tracker for workers pool slots. A
// non-positive count yields an empty tracker whose methods stay no-ops.
func newBatchWorkerOccupancy(workers int) *batchWorkerOccupancy {
	if workers < 0 {
		workers = 0
	}
	return &batchWorkerOccupancy{slots: make([]batchWorkerSlot, workers)}
}

// occupy marks workerID busy on intent starting now. Out-of-range worker
// indexes are ignored: telemetry must never panic the run loop.
func (o *batchWorkerOccupancy) occupy(workerID int, intent reducercontract.Intent) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if workerID < 0 || workerID >= len(o.slots) {
		return
	}
	o.slots[workerID] = batchWorkerSlot{
		busy:      true,
		domain:    intent.Domain,
		intentID:  intent.IntentID,
		startedAt: time.Now(),
	}
}

// release marks workerID idle. Out-of-range worker indexes are ignored.
func (o *batchWorkerOccupancy) release(workerID int) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if workerID < 0 || workerID >= len(o.slots) {
		return
	}
	o.slots[workerID] = batchWorkerSlot{}
}

// snapshot returns the current per-worker state with execution ages measured
// against now. Idle slots report zero age. Negative clock skew clamps to zero
// so skew never pollutes the gauge.
func (o *batchWorkerOccupancy) snapshot(now time.Time) []telemetry.BatchWorkerOccupancy {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	snapshot := make([]telemetry.BatchWorkerOccupancy, len(o.slots))
	for i, slot := range o.slots {
		state := telemetry.BatchWorkerOccupancy{Worker: i}
		if !slot.busy {
			snapshot[i] = state
			continue
		}
		age := now.Sub(slot.startedAt).Seconds()
		if age < 0 {
			age = 0
		}
		snapshot[i] = telemetry.BatchWorkerOccupancy{
			Worker:              i,
			Busy:                true,
			Domain:              string(slot.domain),
			IntentID:            slot.intentID,
			ExecutionAgeSeconds: age,
		}
	}
	return snapshot
}

// InitBatchWorkerOccupancy sizes the per-worker occupancy tracker for the
// configured pool. Production wiring calls it before registering the
// occupancy gauge so every Service copy shares one tracker; runBatchConcurrent
// keeps a lazy fallback for direct and test use. Calling it twice replaces
// the tracker, so it must run before the worker loop starts.
func (s *Service) InitBatchWorkerOccupancy() {
	s.batchOccupancy = newBatchWorkerOccupancy(s.Workers)
}

// BatchWorkerOccupancy returns the current per-worker execution state for the
// eshu_dp_reducer_batch_worker_execution_age_seconds gauge. It implements
// telemetry.BatchWorkerOccupancyObserver. A Service without a tracker reports
// no workers rather than an error, so lanes that never run the batch pool
// stay silent.
func (s Service) BatchWorkerOccupancy(_ context.Context) ([]telemetry.BatchWorkerOccupancy, error) {
	if s.batchOccupancy == nil {
		return nil, nil
	}
	return s.batchOccupancy.snapshot(time.Now()), nil
}

// Compile-time proof that the batch pool's occupancy snapshot feeds the
// telemetry gauge observer contract.
var _ telemetry.BatchWorkerOccupancyObserver = Service{}

// executeAndReportTracked runs one batch intent through executeAndReport
// while marking the worker occupied. The mark brackets execution only: it
// never alters scheduling, locking, timeouts, or claim flow, and the deferred
// release runs on every return path, including shutdown drops after the
// execution returns.
func (s Service) executeAndReportTracked(ctx context.Context, intent reducercontract.Intent, workerID int) (batchAckItem, bool, error) {
	s.occupyBatchWorker(ctx, workerID, intent)
	defer s.releaseBatchWorker(workerID)
	return s.executeAndReport(ctx, intent, workerID)
}

// occupyBatchWorker marks the worker busy and logs the execution start on
// the existing slog family, so a stuck execution's intent id, domain, and
// start time are recoverable from logs alone. The completion line that
// recordReducerResult already emits carries the same worker_id and intent_id
// keys, so the two lines correlate.
func (s Service) occupyBatchWorker(ctx context.Context, workerID int, intent reducercontract.Intent) {
	if s.batchOccupancy == nil {
		return
	}
	s.batchOccupancy.occupy(workerID, intent)
	if s.Logger == nil {
		return
	}
	partitionKey := ""
	if len(intent.EntityKeys) > 0 {
		partitionKey = intent.EntityKeys[0]
	}
	logAttrs := make([]any, 0, 8)
	for _, attr := range telemetry.DomainAttrs(string(intent.Domain), partitionKey) {
		logAttrs = append(logAttrs, attr)
	}
	for _, attr := range telemetry.ScopeAttrs(intent.ScopeID, intent.GenerationID, intent.SourceSystem) {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(logAttrs, log.Queue("reducer"))
	logAttrs = append(logAttrs, log.IntentID(intent.IntentID))
	logAttrs = append(logAttrs, log.WorkerID(fmt.Sprintf("%d", workerID)))
	logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseReduction))
	s.Logger.InfoContext(ctx, "reducer batch worker execution started", logAttrs...)
}

// releaseBatchWorker marks the worker idle after its execution reports.
func (s Service) releaseBatchWorker(workerID int) {
	if s.batchOccupancy == nil {
		return
	}
	s.batchOccupancy.release(workerID)
}

// batchWorkerSpanAttributes names the worker, intent, and domain on the
// existing reducer.run span, so an in-flight execution stuck inside
// executeAndReport shows what it is executing in tracing backends.
func batchWorkerSpanAttributes(intent reducercontract.Intent, workerID int) []attribute.KeyValue {
	return []attribute.KeyValue{
		telemetry.AttrDomain(string(intent.Domain)),
		attribute.String(log.KeyWorkerID, fmt.Sprintf("%d", workerID)),
		attribute.String(log.KeyIntentID, intent.IntentID),
	}
}

// startBatchWorkerSpan starts the reducer.run span for one batch execution.
// It is a thin wrapper keeping the span-attribute list beside the occupancy
// tracker instead of inline at the executeAndReport call site.
func startBatchWorkerSpan(ctx context.Context, tracer trace.Tracer, intent reducercontract.Intent, workerID int) (context.Context, trace.Span) {
	return tracer.Start(ctx, telemetry.SpanReducerRun,
		trace.WithAttributes(batchWorkerSpanAttributes(intent, workerID)...))
}
