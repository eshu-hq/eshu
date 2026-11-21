// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeLeaseOrphanQueueObserver is a queue observer that also reports the
// reducer lease-mirror orphan residue.
type fakeLeaseOrphanQueueObserver struct {
	fakeQueueObserver
	count int64
}

func (f *fakeLeaseOrphanQueueObserver) ReducerLeaseOrphanRows(context.Context) (int64, error) {
	return f.count, nil
}

// TestRegisterObservableGaugesReportsReducerLeaseOrphanRows proves the
// #11081 orphan gauge is registered on the queue-status cadence when the
// queue observer can report it, and that it carries its observed value with
// no labels.
func TestRegisterObservableGaugesReportsReducerLeaseOrphanRows(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	observer := &fakeLeaseOrphanQueueObserver{count: 3}

	if err := RegisterObservableGauges(inst, meter, observer, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ReducerLeaseOrphanRows == nil {
		t.Fatal("expected the reducer lease-orphan gauge to be set")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var saw bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_lease_orphan_rows" {
				continue
			}
			gauge, isGauge := m.Data.(metricdata.Gauge[int64])
			if !isGauge || len(gauge.DataPoints) != 1 {
				t.Fatalf("%s data = %#v, want one int64 point", m.Name, m.Data)
			}
			if got := gauge.DataPoints[0].Value; got != 3 {
				t.Fatalf("%s value = %v, want 3", m.Name, got)
			}
			if gauge.DataPoints[0].Attributes.Len() != 0 {
				t.Fatalf("%s attributes = %v, want none", m.Name, gauge.DataPoints[0].Attributes)
			}
			saw = true
		}
	}
	if !saw {
		t.Fatal("lease-orphan gauge not collected")
	}
}

// TestRegisterObservableGaugesSkipsReducerLeaseOrphanRowsWithoutObserver
// keeps queue observers that cannot report the orphan residue working
// unchanged.
func TestRegisterObservableGaugesSkipsReducerLeaseOrphanRowsWithoutObserver(t *testing.T) {
	t.Parallel()

	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	if err := RegisterObservableGauges(inst, meter, &fakeQueueObserver{}, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ReducerLeaseOrphanRows != nil {
		t.Fatal("reducer lease-orphan gauge registered without an orphan observer")
	}
}

// TestLiveLeaseConflictInstrumentsRegistered pins the #11081 live-lease
// conflict counter registration.
func TestLiveLeaseConflictInstrumentsRegistered(t *testing.T) {
	t.Parallel()

	inst, err := NewInstruments(sdkmetric.NewMeterProvider().Meter("live-lease-conflict-test"))
	require.NoError(t, err)
	assert.NotNil(t, inst.ReducerClaimLiveLeaseConflicts, "ReducerClaimLiveLeaseConflicts counter should be registered")
}

// TestLiveLeaseConflictClaimModesClosed pins the closed claim_mode label
// set. Recording a mode outside this set must fail loudly.
func TestLiveLeaseConflictClaimModesClosed(t *testing.T) {
	t.Parallel()

	want := []string{"batch", "single"}
	if got := LiveLeaseConflictClaimModes(); !slices.Equal(got, want) {
		t.Fatalf("LiveLeaseConflictClaimModes() = %v, want %v", got, want)
	}
}

// TestLiveLeaseConflictConstraintsClosed pins the closed constraint label
// set. Recording a constraint outside this set must fail loudly.
func TestLiveLeaseConflictConstraintsClosed(t *testing.T) {
	t.Parallel()

	want := []string{"main", "mirror"}
	if got := LiveLeaseConflictConstraints(); !slices.Equal(got, want) {
		t.Fatalf("LiveLeaseConflictConstraints() = %v, want %v", got, want)
	}
}
