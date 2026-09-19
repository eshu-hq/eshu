// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fenceGauge returns the last value of an int64 or float64 gauge.
func fenceGauge(t *testing.T, rm metricdata.ResourceMetrics, name string) (float64, bool) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Gauge[int64]:
				if len(data.DataPoints) > 0 {
					return float64(data.DataPoints[len(data.DataPoints)-1].Value), true
				}
			case metricdata.Gauge[float64]:
				if len(data.DataPoints) > 0 {
					return data.DataPoints[len(data.DataPoints)-1].Value, true
				}
			}
		}
	}
	return 0, false
}

// TestInfraInventoryReconcileRunnerRecordsFenceGauges pins the fence's 3 AM
// signal: every cycle, ready or not, records how many repositories carry a
// fence mark and how old the oldest mark is, so an operator can see why
// unscoped infra reads are on the graph (marks exist) and whether the drain
// is progressing, even while the backfill marker is still missing.
func TestInfraInventoryReconcileRunnerRecordsFenceGauges(t *testing.T) {
	instruments, reader := newReconcileTestInstruments(t)
	reconciler := &fakeInfraInventoryReconciler{batches: []InfraInventoryReconcileBatch{
		{Ready: false, DirtyRepos: 3, DirtyOldestAge: 90 * time.Second},
		{Ready: true, DirtyRepos: 0},
	}}
	runner := &InfraInventoryReconcileRunner{
		Reconciler:  reconciler,
		Config:      InfraInventoryReconcileRunnerConfig{PollInterval: time.Minute, RepoBudget: 10},
		Instruments: instruments,
	}

	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	dirty, ok := fenceGauge(t, rm, "eshu_dp_infra_inventory_dirty_repos")
	require.True(t, ok, "dirty repos gauge recorded on a not-ready cycle")
	require.Equal(t, 3.0, dirty)
	age, ok := fenceGauge(t, rm, "eshu_dp_infra_inventory_dirty_oldest_age_seconds")
	require.True(t, ok, "oldest dirty age gauge recorded")
	require.Equal(t, 90.0, age)

	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.NoError(t, reader.Collect(context.Background(), &rm))
	dirty, _ = fenceGauge(t, rm, "eshu_dp_infra_inventory_dirty_repos")
	require.Equal(t, 0.0, dirty, "the gauge falls to zero once the marks drain")
}
