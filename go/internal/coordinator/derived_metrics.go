// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ScannerDerivedPageObservation captures one derived-image discovery page
// admission. Skipped carries the planner skip evidence plus the admission
// skip evidence for the same page, so the reason counter covers the whole
// page in one record call. PageCandidates is the discovery page size;
// Backlog, derived at record time, is the candidates that earned no work
// item this tick (skipped or suppressed).
type ScannerDerivedPageObservation struct {
	InsertedWorkItems int
	Skipped           []schedule.DerivedTargetSkipEvidence
	PageCandidates    int
}

// scannerDerivedPageInstruments holds the derived-image discovery page
// counters registered by newScannerDerivedPageInstruments (#9299).
// Splitting this out of NewMetrics keeps that function under the repo's
// funlen limit.
type scannerDerivedPageInstruments struct {
	admittedTotal metric.Int64Counter
	skippedTotal  metric.Int64Counter
}

// newScannerDerivedPageInstruments registers the derived-image discovery
// page counters (#9299), mirroring the reconcile/reap instrument
// registration pattern used for the rest of this package's coordinator
// metrics.
func newScannerDerivedPageInstruments(meter metric.Meter) (scannerDerivedPageInstruments, error) {
	// Full literal names: verify-telemetry-coverage.sh only recognizes a
	// metric when the constructor's first argument is a string literal, so
	// these three do not use the MetricPrefix concatenation the rest of
	// this file uses.
	admittedTotal, err := meter.Int64Counter(
		"eshu_dp_workflow_coordinator_scanner_derived_admitted_total",
		metric.WithDescription("Total scanner derived-image targets admitted to the work queue (#9299)"),
	)
	if err != nil {
		return scannerDerivedPageInstruments{}, fmt.Errorf("register scanner derived admitted total counter: %w", err)
	}
	skippedTotal, err := meter.Int64Counter(
		"eshu_dp_workflow_coordinator_scanner_derived_skipped_total",
		metric.WithDescription("Total scanner derived-image candidates skipped without admission, by reason (#9299)"),
	)
	if err != nil {
		return scannerDerivedPageInstruments{}, fmt.Errorf("register scanner derived skipped total counter: %w", err)
	}
	return scannerDerivedPageInstruments{
		admittedTotal: admittedTotal,
		skippedTotal:  skippedTotal,
	}, nil
}

// RecordScannerDerivedPage records one derived-image discovery page
// admission (#9299): admitted targets, per-reason skip evidence, and
// the page remainder that earned no work item this tick.
func (m *otelMetrics) RecordScannerDerivedPage(ctx context.Context, observation ScannerDerivedPageObservation) {
	if m == nil {
		return
	}
	inserted := int64(max(observation.InsertedWorkItems, 0))
	m.scannerDerivedAdmittedTotal.Add(ctx, inserted)
	for _, evidence := range observation.Skipped {
		// Reasons are the closed DerivedTargetSkipReason* set the planner
		// and admission layers record; blank reasons and non-positive
		// counts never reach the counter.
		if evidence.Reason == "" || evidence.SkippedCount <= 0 {
			continue
		}
		m.scannerDerivedSkippedTotal.Add(ctx, int64(evidence.SkippedCount),
			metric.WithAttributes(attribute.String(telemetry.MetricDimensionReason, evidence.Reason)))
	}
	remainder := int64(observation.PageCandidates) - inserted
	if remainder < 0 {
		remainder = 0
	}
	m.scannerDerivedBacklog.Store(remainder)
}

// registerCoordinatorGauges registers the observable coordinator gauges and
// the callback that serves them. It lives beside the derived-page
// instruments because the #9299 backlog gauge joined this set; splitting it
// out of NewMetrics keeps that function under the repo's funlen limit.
func registerCoordinatorGauges(meter metric.Meter, recorder *otelMetrics) error {
	desiredGauge, err := meter.Int64ObservableGauge(
		MetricPrefix+"desired_collector_instances",
		metric.WithDescription("Desired workflow coordinator collector instance count"),
	)
	if err != nil {
		return fmt.Errorf("register desired collector instance gauge: %w", err)
	}
	durableGauge, err := meter.Int64ObservableGauge(
		MetricPrefix+"durable_collector_instances",
		metric.WithDescription("Durable workflow coordinator collector instance count"),
	)
	if err != nil {
		return fmt.Errorf("register durable collector instance gauge: %w", err)
	}
	driftGauge, err := meter.Int64ObservableGauge(
		MetricPrefix+"collector_instance_drift",
		metric.WithDescription("Absolute drift between desired and durable collector instance counts"),
	)
	if err != nil {
		return fmt.Errorf("register collector instance drift gauge: %w", err)
	}
	reapedGauge, err := meter.Int64ObservableGauge(
		MetricPrefix+"last_reaped_claims",
		metric.WithDescription("Claims reaped by the most recent workflow coordinator reap pass"),
	)
	if err != nil {
		return fmt.Errorf("register last reaped claims gauge: %w", err)
	}
	reconciledGauge, err := meter.Int64ObservableGauge(
		MetricPrefix+"last_reconciled_runs",
		metric.WithDescription("Runs reconciled by the most recent workflow coordinator run reconciliation pass"),
	)
	if err != nil {
		return fmt.Errorf("register last reconciled runs gauge: %w", err)
	}
	awsFreshnessStuckGauge, err := meter.Int64ObservableGauge(
		MetricPrefix+"aws_freshness_stuck_claimed",
		metric.WithDescription("AWS freshness triggers reclaimed from 'claimed' by the most recent reap pass (#4576)"),
	)
	if err != nil {
		return fmt.Errorf("register AWS freshness stuck-claimed gauge: %w", err)
	}
	gcpFreshnessStuckGauge, err := meter.Int64ObservableGauge(
		MetricPrefix+"gcp_freshness_stuck_claimed",
		metric.WithDescription("GCP freshness triggers reclaimed from 'claimed' by the most recent reap pass (#4576)"),
	)
	if err != nil {
		return fmt.Errorf("register GCP freshness stuck-claimed gauge: %w", err)
	}
	incidentFreshnessStuckGauge, err := meter.Int64ObservableGauge(
		MetricPrefix+"incident_freshness_stuck_claimed",
		metric.WithDescription("Incident freshness triggers reclaimed from 'claimed' by the most recent reap pass (#9379)"),
	)
	if err != nil {
		return fmt.Errorf("register incident freshness stuck-claimed gauge: %w", err)
	}
	scannerDerivedBacklogGauge, err := meter.Int64ObservableGauge(
		"eshu_dp_workflow_coordinator_scanner_derived_backlog",
		metric.WithDescription("Derived-image discovery page candidates that earned no work item on the most recent page (#9299)"),
	)
	if err != nil {
		return fmt.Errorf("register scanner derived backlog gauge: %w", err)
	}
	if _, err := meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		observer.ObserveInt64(desiredGauge, recorder.desiredCount.Load())
		observer.ObserveInt64(durableGauge, recorder.durableCount.Load())
		observer.ObserveInt64(driftGauge, recorder.driftCount.Load())
		observer.ObserveInt64(reapedGauge, recorder.reapedRows.Load())
		observer.ObserveInt64(reconciledGauge, recorder.reconciled.Load())
		observer.ObserveInt64(awsFreshnessStuckGauge, recorder.awsFreshnessStuckClaims.Load())
		observer.ObserveInt64(gcpFreshnessStuckGauge, recorder.gcpFreshnessStuckClaims.Load())
		observer.ObserveInt64(incidentFreshnessStuckGauge, recorder.incidentFreshnessStuckClaims.Load())
		observer.ObserveInt64(scannerDerivedBacklogGauge, recorder.scannerDerivedBacklog.Load())
		return nil
	}, desiredGauge, durableGauge, driftGauge, reapedGauge, reconciledGauge, awsFreshnessStuckGauge, gcpFreshnessStuckGauge, incidentFreshnessStuckGauge, scannerDerivedBacklogGauge); err != nil {
		return fmt.Errorf("register coordinator metrics callback: %w", err)
	}

	return nil
}
