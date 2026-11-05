// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	ownedDerivedSelectedMetric = "eshu_dp_workflow_coordinator_owned_derived_selected_total"
	ownedDerivedSkippedMetric  = "eshu_dp_workflow_coordinator_owned_derived_skipped_total"
)

// TestNewMetricsPublishesOwnedDerivedSignals pins the two #9287 criterion-4
// operator signals: owned-package derived targets selected per ecosystem, and
// skipped candidates labelled by the closed skip-reason set. Counts only:
// package names never reach a label, and non-owned skip evidence never
// reaches these counters.
func TestNewMetricsPublishesOwnedDerivedSignals(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}
	ctx := context.Background()
	recorder.RecordOwnedDerivedPage(ctx, OwnedDerivedObservation{
		CollectorInstanceID: "vuln-intel-1",
		Selected:            map[string]int{"npm": 12, "pypi": 7, "rubygems": 0},
		Skipped: []schedule.DerivedTargetSkipEvidence{
			{
				TargetClass:  schedule.TargetClassOwnedPackage,
				Reason:       schedule.DerivedTargetSkipReasonNonExactVersion,
				SkippedCount: 3,
				Ecosystems:   []string{"npm"},
			},
			{
				TargetClass:  schedule.TargetClassOwnedPackage,
				Reason:       schedule.DerivedTargetSkipReasonBudgetExhausted,
				SkippedCount: 40,
				Ecosystems:   []string{"npm", "pypi"},
			},
			{
				// Findings-derived evidence from the same plan must not
				// leak into the owned counters.
				TargetClass:  schedule.TargetClassFindingCVE,
				Reason:       schedule.DerivedTargetSkipReasonNonCVEIdentifier,
				SkippedCount: 5,
				Ecosystems:   []string{"npm"},
			},
			{
				// Blank reasons and non-positive counts never reach
				// the counter.
				TargetClass: schedule.TargetClassOwnedPackage,
			},
		},
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for ecosystem, want := range map[string]int64{"npm": 12, "pypi": 7} {
		if got := ownedCounterValue(rm, ownedDerivedSelectedMetric, "vuln-intel-1", ecosystem, ""); got != want {
			t.Errorf("selected ecosystem %s = %d, want %d", ecosystem, got, want)
		}
	}
	if got := ownedCounterValue(rm, ownedDerivedSelectedMetric, "vuln-intel-1", "rubygems", ""); got != 0 {
		t.Errorf("selected ecosystem rubygems = %d, want 0 (non-positive counts record nothing)", got)
	}
	cases := []struct {
		ecosystem string
		reason    string
		want      int64
	}{
		{"npm", schedule.DerivedTargetSkipReasonNonExactVersion, 3},
		{"npm", schedule.DerivedTargetSkipReasonBudgetExhausted, 40},
		{"pypi", schedule.DerivedTargetSkipReasonBudgetExhausted, 40},
	}
	for _, tc := range cases {
		if got := ownedCounterValue(rm, ownedDerivedSkippedMetric, "vuln-intel-1", tc.ecosystem, tc.reason); got != tc.want {
			t.Errorf("skipped ecosystem %s reason %s = %d, want %d", tc.ecosystem, tc.reason, got, tc.want)
		}
	}
	if got := ownedCounterValue(rm, ownedDerivedSkippedMetric, "vuln-intel-1", "npm", schedule.DerivedTargetSkipReasonNonCVEIdentifier); got != 0 {
		t.Errorf("findings-derived skip evidence leaked into owned counter: %d", got)
	}
}

// TestServiceRecordsOwnedDerivedSelectionPerPass drives one reconcile tick
// with the owned derivation on and off: an enabled read records the
// per-ecosystem selection for the planning instance, and a disabled
// derivation records nothing (it made no read).
func TestServiceRecordsOwnedDerivedSelectionPerPass(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	const slotProgressOff = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`
	store := newSlotProgressStore(now, -1, slotProgressOff)
	reader := &fakeOwnedPackageTargetReader{targets: makeOwnedNPMTargets(3), window: workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1, SlotSize: 3, RowsRead: 3}}
	metrics := &fakeMetrics{}
	service := slotProgressService(now, slotProgressOff, store, reader, metrics, nil)
	runOneReconcile(t, service)

	if len(metrics.ownedDerivedObserved) != 1 {
		t.Fatalf("owned derived observations = %d, want 1", len(metrics.ownedDerivedObserved))
	}
	observation := metrics.ownedDerivedObserved[0]
	if observation.CollectorInstanceID != "collector-vulnerability-intelligence" {
		t.Errorf("instance = %q, want the planning instance", observation.CollectorInstanceID)
	}
	if got := observation.Selected["npm"]; got != 3 {
		t.Errorf("selected npm = %d, want 3", got)
	}

	const derivationOff = `{"targets":[
		{"source":"nvd","scope_id":"vuln-intel://nvd/cve","cve_ids":["CVE-2026-0001"],"results_per_page":100}
	],"derive_from_owned_packages":{"enabled":false,"sources":["osv"],"ecosystems":["npm"]}}`
	offStore := newSlotProgressStore(now, -1, derivationOff)
	offMetrics := &fakeMetrics{}
	offService := slotProgressService(now, derivationOff, offStore,
		&fakeOwnedPackageTargetReader{}, offMetrics, nil)
	runOneReconcile(t, offService)

	if len(offMetrics.ownedDerivedObserved) != 0 {
		t.Fatalf("owned derived observations with derivation off = %d, want 0", len(offMetrics.ownedDerivedObserved))
	}
}

func ownedCounterValue(rm metricdata.ResourceMetrics, name, instance, ecosystem, reason string) int64 {
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				gotInstance, _ := dp.Attributes.Value(telemetry.MetricDimensionCollectorInstanceID)
				if gotInstance.AsString() != instance {
					continue
				}
				gotEcosystem, _ := dp.Attributes.Value(telemetry.MetricDimensionEcosystem)
				if gotEcosystem.AsString() != ecosystem {
					continue
				}
				if reason != "" {
					gotReason, _ := dp.Attributes.Value(telemetry.MetricDimensionReason)
					if gotReason.AsString() != reason {
						continue
					}
				}
				total += dp.Value
			}
		}
	}
	return total
}
