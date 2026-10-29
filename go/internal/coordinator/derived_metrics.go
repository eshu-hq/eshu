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
