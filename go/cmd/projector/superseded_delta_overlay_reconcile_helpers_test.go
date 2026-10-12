// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// Helpers for the #7978 reconcile proofs in superseded_delta_overlay_live_test.go.

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gitcollector "github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// withDriftReader wires telemetry instruments on a manual reader into the
// harness's canonical writer; call it before buildService.
func (h *overlayHarness) withDriftReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("proof-7978"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	h.instruments = instruments
	return reader
}

// driftRetractions sums eshu_dp_reconciliation_drift_retractions_total.
func driftRetractions(ctx context.Context, t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, recorded := range scopeMetrics.Metrics {
			if recorded.Name != "eshu_dp_reconciliation_drift_retractions_total" {
				continue
			}
			if sum, ok := recorded.Data.(metricdata.Sum[int64]); ok {
				for _, point := range sum.DataPoints {
					total += point.Value
				}
			}
		}
	}
	return total
}

// sweepDecision runs the production reconcile decision with a fleet reindex
// watermark and a 24 h interval.
func (h *overlayHarness) sweepDecision(ctx context.Context, watermark time.Time) (bool, string) {
	return gitcollector.ReconcileSweepDecision(ctx, h.store, 24*time.Hour, watermark, time.Time{}, time.Now().UTC(), h.scope.ScopeID, nil)
}

// gitReindexReason is the fleet reindex reason the collector forces with.
const gitReindexReason = scope.ReconcileReasonReindexRequested

// reasonFor returns the forced reason the next commit carries: a one-shot
// override set by commitWithReason, or the production sweep decision.
func (h *overlayHarness) reasonFor(ctx context.Context, gen overlayGen) string {
	h.mu.Lock()
	override := h.reasonOverride
	h.reasonOverride = ""
	h.mu.Unlock()
	if override != "" {
		return override
	}
	return h.reconcileReason(ctx, gen)
}

// commitWithReason commits gen with an explicit forced reason, as the
// collector does when a reindex watermark decides.
func (h *overlayHarness) commitWithReason(ctx context.Context, gen overlayGen, reason string) error {
	h.mu.Lock()
	h.reasonOverride = reason
	h.mu.Unlock()
	return h.commit(ctx, gen)
}

// generationCount returns the scope's scope_generations row count.
func (h *overlayHarness) generationCount(ctx context.Context) int {
	h.t.Helper()
	var n int
	if err := h.sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM scope_generations WHERE scope_id = $1`,
		h.scope.ScopeID).Scan(&n); err != nil {
		h.t.Fatalf("count generations: %v", err)
	}
	return n
}

// waitReprojected waits until generationID's projector work row was re-opened
// by a #7978 reconcile at least reopens times and has succeeded again.
func (h *overlayHarness) waitReprojected(ctx context.Context, generationID string, reopens int) {
	h.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var status string
	var got int
	for time.Now().Before(deadline) {
		_ = h.sqlDB.QueryRowContext(ctx, `SELECT status, COALESCE((payload ->> 'reconcile_reopen_count')::int, 0)
FROM fact_work_items WHERE stage = 'projector' AND generation_id = $1`, generationID).Scan(&status, &got)
		if status == "succeeded" && got >= reopens {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.logGenerations(ctx)
	h.t.Fatalf("generation %s work status=%q reopens=%d, want succeeded after %d re-opens", generationID, status, got, reopens)
}

// overlayIntentRows counts the rows a projection emits downstream of the
// canonical graph: reducer work items and shared projection intents.
type overlayIntentRows struct {
	reducerWorkItems    int
	reducerSucceeded    int
	sharedIntents       int
	sharedIntentsClosed int
	contentFiles        int
}

// intentRows reads the scope's reducer work items and shared projection
// intents.
func (h *overlayHarness) intentRows(ctx context.Context) overlayIntentRows {
	h.t.Helper()
	var rows overlayIntentRows
	if err := h.sqlDB.QueryRowContext(ctx, `
SELECT count(*), count(*) FILTER (WHERE status = 'succeeded')
FROM fact_work_items WHERE stage = 'reducer' AND scope_id = $1`, h.scope.ScopeID).Scan(&rows.reducerWorkItems, &rows.reducerSucceeded); err != nil {
		h.t.Fatalf("count reducer work items: %v", err)
	}
	if err := h.sqlDB.QueryRowContext(ctx, `
SELECT count(*), count(*) FILTER (WHERE completed_at IS NOT NULL)
FROM shared_projection_intents WHERE scope_id = $1`, h.scope.ScopeID).Scan(&rows.sharedIntents, &rows.sharedIntentsClosed); err != nil {
		h.t.Fatalf("count shared projection intents: %v", err)
	}
	if err := h.sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM content_files WHERE repo_id = $1`,
		h.repo.ID).Scan(&rows.contentFiles); err != nil {
		h.t.Fatalf("count content files: %v", err)
	}
	return rows
}
