// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	freshnessReapOutcomeSuccess    = "success"
	freshnessReapOutcomeError      = "error"
	coordinatorMetricPrefix        = "eshu_dp_workflow_coordinator_"
)

// Metrics records coordinator reconcile-loop telemetry.
type Metrics interface {
	RecordReconcile(context.Context, ReconcileObservation)
	RecordReap(context.Context, ReapObservation)
	RecordRunReconciliation(context.Context, RunReconciliationObservation)
	// RecordAWSFreshnessReap records one AWS freshness stuck-claim reap pass
	// (#4576): the operator-visible signal for triggers stranded at 'claimed'
	// by a mid-batch handoff abort or a coordinator crash, and how many the
	// most recent pass reclaimed back to 'queued'.
	RecordAWSFreshnessReap(context.Context, FreshnessReapObservation)
	// RecordGCPFreshnessReap is RecordAWSFreshnessReap's GCP counterpart,
	// mirroring the identical AWS/GCP freshness trigger shape (#4576).
	RecordGCPFreshnessReap(context.Context, FreshnessReapObservation)
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

// FreshnessReapObservation captures one AWS/GCP freshness-trigger
// expired-claim-lease reap pass (#4576). ReclaimedCount is both the number of
// triggers requeued this pass and, taken over time, the operator-visible
// stuck-claimed rate: a healthy deployment reclaims ~0 triggers per pass
// because handoffs normally complete before the lease expires.
type FreshnessReapObservation struct {
	Outcome        string
	Duration       time.Duration
	ReclaimedCount int
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

	awsFreshnessReapTotal    metric.Int64Counter
	awsFreshnessReapDuration metric.Float64Histogram
	gcpFreshnessReapTotal    metric.Int64Counter
	gcpFreshnessReapDuration metric.Float64Histogram

	desiredCount            atomic.Int64
	durableCount            atomic.Int64
	driftCount              atomic.Int64
	reapedRows              atomic.Int64
	reconciled              atomic.Int64
	awsFreshnessStuckClaims atomic.Int64
	gcpFreshnessStuckClaims atomic.Int64
}

// freshnessReapMetricInstruments holds the AWS/GCP freshness stuck-claim reap
// counters and histograms registered by newFreshnessReapInstruments (#4576).
// Splitting this out of NewMetrics keeps that function under the repo's
// funlen limit.
type freshnessReapMetricInstruments struct {
	awsReapTotal    metric.Int64Counter
	awsReapDuration metric.Float64Histogram
	gcpReapTotal    metric.Int64Counter
	gcpReapDuration metric.Float64Histogram
}

// newFreshnessReapInstruments registers the AWS/GCP freshness stuck-claim
// reap counters and histograms (#4576), mirroring the reconcile/reap
// instrument registration pattern used for the rest of this package's
// coordinator metrics.
func newFreshnessReapInstruments(meter metric.Meter) (freshnessReapMetricInstruments, error) {
	awsFreshnessReapTotal, err := meter.Int64Counter(
		coordinatorMetricPrefix+"aws_freshness_reap_total",
		metric.WithDescription("Total AWS freshness stuck-claim reap passes (#4576)"),
	)
	if err != nil {
		return freshnessReapMetricInstruments{}, fmt.Errorf("register AWS freshness reap total counter: %w", err)
	}
	awsFreshnessReapDuration, err := meter.Float64Histogram(
		coordinatorMetricPrefix+"aws_freshness_reap_duration_seconds",
		metric.WithDescription("AWS freshness stuck-claim reap pass duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return freshnessReapMetricInstruments{}, fmt.Errorf("register AWS freshness reap duration histogram: %w", err)
	}
	gcpFreshnessReapTotal, err := meter.Int64Counter(
		coordinatorMetricPrefix+"gcp_freshness_reap_total",
		metric.WithDescription("Total GCP freshness stuck-claim reap passes (#4576)"),
	)
	if err != nil {
		return freshnessReapMetricInstruments{}, fmt.Errorf("register GCP freshness reap total counter: %w", err)
	}
	gcpFreshnessReapDuration, err := meter.Float64Histogram(
		coordinatorMetricPrefix+"gcp_freshness_reap_duration_seconds",
		metric.WithDescription("GCP freshness stuck-claim reap pass duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return freshnessReapMetricInstruments{}, fmt.Errorf("register GCP freshness reap duration histogram: %w", err)
	}
	return freshnessReapMetricInstruments{
		awsReapTotal:    awsFreshnessReapTotal,
		awsReapDuration: awsFreshnessReapDuration,
		gcpReapTotal:    gcpFreshnessReapTotal,
		gcpReapDuration: gcpFreshnessReapDuration,
	}, nil
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
	freshnessReapInstruments, err := newFreshnessReapInstruments(meter)
	if err != nil {
		return nil, err
	}

	recorder := &otelMetrics{
		reconcileTotal:           reconcileTotal,
		reconcileDuration:        reconcileDuration,
		reapTotal:                reapTotal,
		reapDuration:             reapDuration,
		runReconcileTotal:        runReconcileTotal,
		runReconcileDur:          runReconcileDur,
		awsFreshnessReapTotal:    freshnessReapInstruments.awsReapTotal,
		awsFreshnessReapDuration: freshnessReapInstruments.awsReapDuration,
		gcpFreshnessReapTotal:    freshnessReapInstruments.gcpReapTotal,
		gcpFreshnessReapDuration: freshnessReapInstruments.gcpReapDuration,
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
	awsFreshnessStuckGauge, err := meter.Int64ObservableGauge(
		coordinatorMetricPrefix+"aws_freshness_stuck_claimed",
		metric.WithDescription("AWS freshness triggers reclaimed from 'claimed' by the most recent reap pass (#4576)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWS freshness stuck-claimed gauge: %w", err)
	}
	gcpFreshnessStuckGauge, err := meter.Int64ObservableGauge(
		coordinatorMetricPrefix+"gcp_freshness_stuck_claimed",
		metric.WithDescription("GCP freshness triggers reclaimed from 'claimed' by the most recent reap pass (#4576)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GCP freshness stuck-claimed gauge: %w", err)
	}
	if _, err := meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		observer.ObserveInt64(desiredGauge, recorder.desiredCount.Load())
		observer.ObserveInt64(durableGauge, recorder.durableCount.Load())
		observer.ObserveInt64(driftGauge, recorder.driftCount.Load())
		observer.ObserveInt64(reapedGauge, recorder.reapedRows.Load())
		observer.ObserveInt64(reconciledGauge, recorder.reconciled.Load())
		observer.ObserveInt64(awsFreshnessStuckGauge, recorder.awsFreshnessStuckClaims.Load())
		observer.ObserveInt64(gcpFreshnessStuckGauge, recorder.gcpFreshnessStuckClaims.Load())
		return nil
	}, desiredGauge, durableGauge, driftGauge, reapedGauge, reconciledGauge, awsFreshnessStuckGauge, gcpFreshnessStuckGauge); err != nil {
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

func (m *otelMetrics) RecordAWSFreshnessReap(ctx context.Context, observation FreshnessReapObservation) {
	if m == nil {
		return
	}
	outcome := observation.Outcome
	if outcome != freshnessReapOutcomeSuccess {
		outcome = freshnessReapOutcomeError
	}
	attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
	m.awsFreshnessReapTotal.Add(ctx, 1, attrs)
	m.awsFreshnessReapDuration.Record(ctx, observation.Duration.Seconds(), attrs)
	if outcome == freshnessReapOutcomeSuccess {
		m.awsFreshnessStuckClaims.Store(int64(max(observation.ReclaimedCount, 0)))
	}
}

func (m *otelMetrics) RecordGCPFreshnessReap(ctx context.Context, observation FreshnessReapObservation) {
	if m == nil {
		return
	}
	outcome := observation.Outcome
	if outcome != freshnessReapOutcomeSuccess {
		outcome = freshnessReapOutcomeError
	}
	attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
	m.gcpFreshnessReapTotal.Add(ctx, 1, attrs)
	m.gcpFreshnessReapDuration.Record(ctx, observation.Duration.Seconds(), attrs)
	if outcome == freshnessReapOutcomeSuccess {
		m.gcpFreshnessStuckClaims.Store(int64(max(observation.ReclaimedCount, 0)))
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

func max(value int, minimum int) int { //nolint:gocritic // builtinShadowDecl: helper named after the math/max builtin to keep call sites readable.
	if value < minimum {
		return minimum
	}
	return value
}
