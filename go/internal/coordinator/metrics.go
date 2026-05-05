package coordinator

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	reconcileOutcomeSuccess        = "success"
	reconcileOutcomeReconcileError = "reconcile_error"
	reconcileOutcomeStateReadError = "state_read_error"
	reaperOutcomeSuccess           = "success"
	reaperOutcomeError             = "error"
	runReconcileOutcomeSuccess     = "success"
	runReconcileOutcomeError       = "error"
	coordinatorMetricPrefix        = "eshu_dp_workflow_coordinator_"
)

// Metrics records coordinator reconcile-loop telemetry.
type Metrics interface {
	RecordReconcile(context.Context, ReconcileObservation)
	RecordReap(context.Context, ReapObservation)
	RecordRunReconciliation(context.Context, RunReconciliationObservation)
}

// ReconcileObservation captures one reconcile-loop outcome.
type ReconcileObservation struct {
	Outcome      string
	Duration     time.Duration
	DesiredCount int
	DurableCount int
}

// ReapObservation captures one expired-claim reap pass.
type ReapObservation struct {
	Outcome    string
	Duration   time.Duration
	ReapedRows int
}

// RunReconciliationObservation captures one workflow progress reconciliation pass.
type RunReconciliationObservation struct {
	Outcome        string
	Duration       time.Duration
	ReconciledRuns int
}

type otelMetrics struct {
	reconcileTotal    metric.Int64Counter
	reconcileDuration metric.Float64Histogram
	reapTotal         metric.Int64Counter
	reapDuration      metric.Float64Histogram
	runReconcileTotal metric.Int64Counter
	runReconcileDur   metric.Float64Histogram

	desiredCount atomic.Int64
	durableCount atomic.Int64
	driftCount   atomic.Int64
	reapedRows   atomic.Int64
	reconciled   atomic.Int64
}

// NewMetrics registers coordinator-specific OTEL instruments.
func NewMetrics(meter metric.Meter) (Metrics, error) {
	if meter == nil {
		return nil, fmt.Errorf("meter is required")
	}

	reconcileTotal, err := meter.Int64Counter(
		coordinatorMetricPrefix+"reconcile_total",
		metric.WithDescription("Total workflow coordinator reconcile loop executions"),
	)
	if err != nil {
		return nil, fmt.Errorf("register reconcile total counter: %w", err)
	}
	reconcileDuration, err := meter.Float64Histogram(
		coordinatorMetricPrefix+"reconcile_duration_seconds",
		metric.WithDescription("Workflow coordinator reconcile loop duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return nil, fmt.Errorf("register reconcile duration histogram: %w", err)
	}
	reapTotal, err := meter.Int64Counter(
		coordinatorMetricPrefix+"reap_total",
		metric.WithDescription("Total workflow coordinator expired-claim reap passes"),
	)
	if err != nil {
		return nil, fmt.Errorf("register reap total counter: %w", err)
	}
	reapDuration, err := meter.Float64Histogram(
		coordinatorMetricPrefix+"reap_duration_seconds",
		metric.WithDescription("Workflow coordinator expired-claim reap duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return nil, fmt.Errorf("register reap duration histogram: %w", err)
	}
	runReconcileTotal, err := meter.Int64Counter(
		coordinatorMetricPrefix+"run_reconcile_total",
		metric.WithDescription("Total workflow coordinator workflow-run reconciliation passes"),
	)
	if err != nil {
		return nil, fmt.Errorf("register run reconcile total counter: %w", err)
	}
	runReconcileDur, err := meter.Float64Histogram(
		coordinatorMetricPrefix+"run_reconcile_duration_seconds",
		metric.WithDescription("Workflow coordinator workflow-run reconciliation duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return nil, fmt.Errorf("register run reconcile duration histogram: %w", err)
	}

	recorder := &otelMetrics{
		reconcileTotal:    reconcileTotal,
		reconcileDuration: reconcileDuration,
		reapTotal:         reapTotal,
		reapDuration:      reapDuration,
		runReconcileTotal: runReconcileTotal,
		runReconcileDur:   runReconcileDur,
	}

	desiredGauge, err := meter.Int64ObservableGauge(
		coordinatorMetricPrefix+"desired_collector_instances",
		metric.WithDescription("Desired workflow coordinator collector instance count"),
	)
	if err != nil {
		return nil, fmt.Errorf("register desired collector instance gauge: %w", err)
	}
	durableGauge, err := meter.Int64ObservableGauge(
		coordinatorMetricPrefix+"durable_collector_instances",
		metric.WithDescription("Durable workflow coordinator collector instance count"),
	)
	if err != nil {
		return nil, fmt.Errorf("register durable collector instance gauge: %w", err)
	}
	driftGauge, err := meter.Int64ObservableGauge(
		coordinatorMetricPrefix+"collector_instance_drift",
		metric.WithDescription("Absolute drift between desired and durable collector instance counts"),
	)
	if err != nil {
		return nil, fmt.Errorf("register collector instance drift gauge: %w", err)
	}
	reapedGauge, err := meter.Int64ObservableGauge(
		coordinatorMetricPrefix+"last_reaped_claims",
		metric.WithDescription("Claims reaped by the most recent workflow coordinator reap pass"),
	)
	if err != nil {
		return nil, fmt.Errorf("register last reaped claims gauge: %w", err)
	}
	reconciledGauge, err := meter.Int64ObservableGauge(
		coordinatorMetricPrefix+"last_reconciled_runs",
		metric.WithDescription("Runs reconciled by the most recent workflow coordinator run reconciliation pass"),
	)
	if err != nil {
		return nil, fmt.Errorf("register last reconciled runs gauge: %w", err)
	}
	if _, err := meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		observer.ObserveInt64(desiredGauge, recorder.desiredCount.Load())
		observer.ObserveInt64(durableGauge, recorder.durableCount.Load())
		observer.ObserveInt64(driftGauge, recorder.driftCount.Load())
		observer.ObserveInt64(reapedGauge, recorder.reapedRows.Load())
		observer.ObserveInt64(reconciledGauge, recorder.reconciled.Load())
		return nil
	}, desiredGauge, durableGauge, driftGauge, reapedGauge, reconciledGauge); err != nil {
		return nil, fmt.Errorf("register coordinator metrics callback: %w", err)
	}

	return recorder, nil
}

func (m *otelMetrics) RecordReap(ctx context.Context, observation ReapObservation) {
	if m == nil {
		return
	}
	outcome := observation.Outcome
	if outcome != reaperOutcomeSuccess {
		outcome = reaperOutcomeError
	}
	attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
	m.reapTotal.Add(ctx, 1, attrs)
	m.reapDuration.Record(ctx, observation.Duration.Seconds(), attrs)
	if outcome == reaperOutcomeSuccess {
		m.reapedRows.Store(int64(max(observation.ReapedRows, 0)))
	}
}

func (m *otelMetrics) RecordRunReconciliation(ctx context.Context, observation RunReconciliationObservation) {
	if m == nil {
		return
	}
	outcome := observation.Outcome
	if outcome != runReconcileOutcomeSuccess {
		outcome = runReconcileOutcomeError
	}
	attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
	m.runReconcileTotal.Add(ctx, 1, attrs)
	m.runReconcileDur.Record(ctx, observation.Duration.Seconds(), attrs)
	if outcome == runReconcileOutcomeSuccess {
		m.reconciled.Store(int64(max(observation.ReconciledRuns, 0)))
	}
}

func (m *otelMetrics) RecordReconcile(ctx context.Context, observation ReconcileObservation) {
	if m == nil {
		return
	}

	outcome := observation.Outcome
	switch outcome {
	case reconcileOutcomeSuccess, reconcileOutcomeReconcileError, reconcileOutcomeStateReadError:
	default:
		outcome = reconcileOutcomeReconcileError
	}
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	)
	m.reconcileTotal.Add(ctx, 1, attrs)
	m.reconcileDuration.Record(ctx, observation.Duration.Seconds(), attrs)

	if outcome != reconcileOutcomeSuccess {
		return
	}

	desired := int64(max(observation.DesiredCount, 0))
	durable := int64(max(observation.DurableCount, 0))
	drift := desired - durable
	if drift < 0 {
		drift = -drift
	}
	m.desiredCount.Store(desired)
	m.durableCount.Store(durable)
	m.driftCount.Store(drift)
}

func max(value int, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
