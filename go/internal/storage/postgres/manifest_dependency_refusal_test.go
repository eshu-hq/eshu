// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The refusing read is reached through a runtime interface assertion, so a
// drifted signature would skip retracting consumers silently. This pins the
// production FactStore on the seam.
var _ correlation.ManifestDependencyRefusingLoader = (*FactStore)(nil)

// TestListActivePackageManifestDependencyFactsWithRefusalsFailsOnAMissingBase
// pins #10109 F1 on a stub queryer: the intent scope is delta-active and the
// base lookup finds no full generation at its commit. The refusing read fails
// with the non-retryable ManifestDeltaBaseRefusedError before returning rows,
// so a retracting consumer never sees the delta-only fallback. The default
// delta-only read on the same stub keeps its row (pinned by
// TestListActiveCrossplaneSatisfiedByFactsFailsOnAMissingBase).
func TestListActivePackageManifestDependencyFactsWithRefusalsFailsOnAMissingBase(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0)}}, // active page
		crossplaneRefusalTarget,                       // delta-target lookup
		{},                                            // delta-base page
		{},                                            // base lookup: no base
	}}
	loaded, refused, err := NewFactStore(db).WithInstruments(instruments).ListActivePackageManifestDependencyFactsWithRefusals(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	var refusal *ManifestDeltaBaseRefusedError
	if !errors.As(err, &refusal) {
		t.Fatalf("read error = %v, want *ManifestDeltaBaseRefusedError", err)
	}
	if !refused {
		t.Fatal("refused = false, want true for a missing delta base")
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded rows = %d, want 0: the refusing read returns no fallback rows", len(loaded))
	}
	if refusal.ScopeID != "repository:repo-1" || refusal.GenerationID != "generation-1" ||
		refusal.Outcome != deltaOverlayBaseMissing {
		t.Fatalf("refusal = %+v, want repo-1 generation-1 union_base_missing", refusal)
	}
	if reducercontract.IsRetryable(err) {
		t.Fatal("IsRetryable = true, want false: a retry reads the same refused base")
	}
	if got := refusal.FailureClass(); got != ManifestDependencyDeltaBaseRefusedFailureClass {
		t.Fatalf("FailureClass = %q, want %q", got, ManifestDependencyDeltaBaseRefusedFailureClass)
	}
	// The refusing read runs the same four statements as the default
	// delta-only read on this shape: no extra statement for the refusal.
	if got := len(db.queries); got != 4 {
		t.Fatalf("statements = %d, want 4: the refusal adds no read", got)
	}
	if got := deltaUnionOutcomeCount(t, reader, deltaOverlayBaseMissing); got != 1 {
		t.Fatalf("union_base_missing = %d, want 1 counted refusal", got)
	}
}

// deltaUnionOutcomeCount sums the manifest delta-union counter for one
// outcome from a manual-reader collection.
func deltaUnionOutcomeCount(t *testing.T, reader *metric.ManualReader, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_package_manifest_dependency_read_delta_union_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				got, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				if got.AsString() == outcome {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// TestListActivePackageManifestDependencyFactsWithRefusalsFailsOnAScopeMismatch
// pins #10109 F2 on a stub queryer: the base resolves but the delta carries
// no delta_generation repository fact, so the overlay would run additively.
// The refusing read fails with union_scope_mismatch instead of returning the
// additive union.
func TestListActivePackageManifestDependencyFactsWithRefusalsFailsOnAScopeMismatch(t *testing.T) {
	t.Parallel()

	baseRow := manifestDependencyFakeRow(0)
	baseRow[2] = "base-1"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0)}},                                  // active page
		{rows: [][]any{{"repository:repo-1", "generation-1", "c0", "", `["base-1"]`}}}, // delta-target lookup
		{rows: [][]any{baseRow}},                                                       // delta-base page
		{rows: [][]any{{"repository:repo-1", "base-1", "active"}}},                     // base lookup
		{rows: [][]any{manifestDependencyFakeRow(1)}},                                  // overlay inputs: no delta fact
	}}
	_, refused, err := NewFactStore(db).ListActivePackageManifestDependencyFactsWithRefusals(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	var refusal *ManifestDeltaBaseRefusedError
	if !errors.As(err, &refusal) {
		t.Fatalf("read error = %v, want *ManifestDeltaBaseRefusedError", err)
	}
	if !refused {
		t.Fatal("refused = false, want true for a scope mismatch")
	}
	if refusal.Outcome != deltaOverlayScopeMismatch {
		t.Fatalf("refusal outcome = %q, want %q", refusal.Outcome, deltaOverlayScopeMismatch)
	}
	if reducercontract.IsRetryable(err) {
		t.Fatal("IsRetryable = true, want false for a scope mismatch")
	}
}

// TestListActivePackageManifestDependencyFactsWithRefusalsRetriesAFailedProbe
// pins the #10109 probe refinement: the base resolves to a superseded full
// generation and the retention-probe statement fails. The base may be intact,
// so the read fails with a retryable error instead of the dead-lettering
// refusal.
func TestListActivePackageManifestDependencyFactsWithRefusalsRetriesAFailedProbe(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0)}},                                  // active page
		{rows: [][]any{{"repository:repo-1", "generation-1", "c0", "", `["base-1"]`}}}, // delta-target lookup
		{}, // delta-base page
		{rows: [][]any{{"repository:repo-1", "base-1", "superseded"}}}, // base lookup
		{err: context.DeadlineExceeded},                                // retention probe
	}}
	_, refused, err := NewFactStore(db).ListActivePackageManifestDependencyFactsWithRefusals(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	if err == nil {
		t.Fatal("read error = nil, want the retryable probe failure")
	}
	if refused {
		t.Fatal("refused = true, want false: a failed probe is not a refusal")
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatalf("IsRetryable(%v) = false, want true for a failed probe statement", err)
	}
	var refusal *ManifestDeltaBaseRefusedError
	if errors.As(err, &refusal) {
		t.Fatalf("read error = %v, want no ManifestDeltaBaseRefusedError for a probe failure", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read error = %v, want it to wrap the probe failure", err)
	}
}

// TestListActivePackageManifestDependencyFactsWithRefusalsKeepsDeltaOnlyWhenWhole
// pins the #10109 count-and-continue posture: a legacy delta with no baseline
// (union_no_baseline) and a resolved-but-unmatched base
// (union_base_unmatched) keep their delta rows with refused=false. Only
// union_base_missing and union_scope_mismatch refuse.
func TestListActivePackageManifestDependencyFactsWithRefusalsKeepsDeltaOnlyWhenWhole(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0)}},                        // active page
		{rows: [][]any{{"repository:repo-1", "generation-1", "", "", `[]`}}}, // delta-target lookup: no baseline
	}}
	loaded, refused, err := NewFactStore(db).ListActivePackageManifestDependencyFactsWithRefusals(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	if err != nil {
		t.Fatalf("read error = %v, want nil for a legacy delta", err)
	}
	if refused {
		t.Fatal("refused = true, want false for union_no_baseline")
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded rows = %d, want the 1 delta row", len(loaded))
	}
}
