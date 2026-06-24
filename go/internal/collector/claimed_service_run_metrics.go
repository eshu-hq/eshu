package collector

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// recordClaimRunDuration records the wall time of one claimed-service
// processing cycle (processClaimed) in seconds, labeled by collector_kind,
// source_system, and a bounded outcome token. It must be called on every
// return path — callers use a deferred wrapper (recordClaimRunDurationDeferred)
// so no cycle is silently dropped.
//
// Concurrency safety: metric.Float64Histogram.Record is safe for concurrent
// callers per the OTEL Go SDK specification. The startedAt and outcome values
// are call-local (stack frame), so N concurrent workers never share mutable
// state through this function.
func (s ClaimedService) recordClaimRunDuration(
	ctx context.Context,
	item workflow.WorkItem,
	startedAt time.Time,
	outcome string,
) {
	if s.Instruments == nil || s.Instruments.WorkflowClaimRunDuration == nil {
		return
	}
	elapsed := s.now().Sub(startedAt).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	s.Instruments.WorkflowClaimRunDuration.Record(ctx, elapsed, metric.WithAttributes(
		telemetry.AttrCollectorKind(string(s.CollectorKind)),
		telemetry.AttrSourceSystem(item.SourceSystem),
		telemetry.AttrOutcome(outcome),
	))
}

// recordClaimFactsEmitted records the number of facts emitted in a successful
// claimed-service run, labeled by collector_kind and source_system.
// CollectedGeneration.FactCount is an estimated total already populated by
// every collector at the seam; no extra scan or IO is introduced.
//
// Only called on the success path (after commitCollected returns nil). Unchanged,
// released, and failed runs contribute zero facts and are not counted here —
// they are visible via the outcome label on WorkflowClaimRunDuration.
//
// Concurrency safety: metric.Int64Counter.Add is safe for concurrent callers.
func (s ClaimedService) recordClaimFactsEmitted(
	ctx context.Context,
	item workflow.WorkItem,
	collected CollectedGeneration,
) {
	if s.Instruments == nil || s.Instruments.WorkflowClaimFactsEmitted == nil {
		return
	}
	count := int64(collected.FactCount)
	if count <= 0 {
		return
	}
	s.Instruments.WorkflowClaimFactsEmitted.Add(ctx, count, metric.WithAttributes(
		telemetry.AttrCollectorKind(string(s.CollectorKind)),
		telemetry.AttrSourceSystem(item.SourceSystem),
	))
}
