// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// writeMarkerMetrics holds the write-marker deferral counter values and
// wait-histogram counts, keyed by outcome.
type writeMarkerMetrics struct {
	deferrals map[string]int64
	waits     map[string]uint64
}

func collectWriteMarkerMetrics(t *testing.T, reader *sdkmetric.ManualReader) writeMarkerMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := writeMarkerMetrics{deferrals: map[string]int64{}, waits: map[string]uint64{}}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "eshu_dp_projector_write_marker_deferrals_total":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s data = %T, want Sum[int64]", m.Name, m.Data)
				}
				for _, point := range sum.DataPoints {
					outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
					got.deferrals[outcome.AsString()] += point.Value
				}
			case "eshu_dp_projector_write_marker_wait_seconds":
				hist, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("%s data = %T, want Histogram[float64]", m.Name, m.Data)
				}
				for _, point := range hist.DataPoints {
					outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
					got.waits[outcome.AsString()] += point.Count
				}
			}
		}
	}
	return got
}

// TestMarkProjectionWriteStartedRecordsDeferralAndWaitOutcomes proves every
// deferred marker attempt is counted with what the loop did next, and every
// marker that waited records one wait-duration point with its terminal
// outcome. A marker that never waited records nothing, so the histogram
// measures only busy-generation waits.
func TestMarkProjectionWriteStartedRecordsDeferralAndWaitOutcomes(t *testing.T) {
	t.Parallel()

	deferred := fmt.Errorf("lock timeout: %w", failure.ErrWorkWriteMarkerDeferred)
	superseded := fmt.Errorf("generation retired: %w", failure.ErrWorkSuperseded)
	claimLost := fmt.Errorf("stale attempt: %w", failure.ErrWorkClaimLost)
	markerFailure := errors.New("connection reset")
	exhausted := make([]error, DefaultWriteMarkerMaxAttempts)
	for i := range exhausted {
		exhausted[i] = deferred
	}
	tests := []struct {
		name          string
		markerErrs    []error
		wantErr       error
		wantDeferrals map[string]int64
		wantWaits     map[string]uint64
	}{
		{
			name:          "no deferral records nothing",
			markerErrs:    nil,
			wantDeferrals: map[string]int64{},
			wantWaits:     map[string]uint64{},
		},
		{
			name:          "deferred then written",
			markerErrs:    []error{deferred, deferred},
			wantDeferrals: map[string]int64{"retried": 2},
			wantWaits:     map[string]uint64{"written": 1},
		},
		{
			name:          "bound exhausted gives up",
			markerErrs:    exhausted,
			wantErr:       failure.ErrWorkWriteMarkerDeferred,
			wantDeferrals: map[string]int64{"retried": DefaultWriteMarkerMaxAttempts - 1, "gave_up": 1},
			wantWaits:     map[string]uint64{"gave_up": 1},
		},
		{
			name:          "deferred then superseded",
			markerErrs:    []error{deferred, superseded},
			wantErr:       failure.ErrWorkSuperseded,
			wantDeferrals: map[string]int64{"retried": 1},
			wantWaits:     map[string]uint64{"superseded": 1},
		},
		{
			name:          "deferred then claim lost",
			markerErrs:    []error{deferred, claimLost},
			wantErr:       failure.ErrWorkClaimLost,
			wantDeferrals: map[string]int64{"retried": 1},
			wantWaits:     map[string]uint64{"claim_lost": 1},
		},
		{
			name:          "marker error after waiting fails",
			markerErrs:    []error{deferred, markerFailure},
			wantErr:       markerFailure,
			wantDeferrals: map[string]int64{"retried": 1},
			wantWaits:     map[string]uint64{"failed": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader, instruments := newAckWaitReader(t)
			marker := &fakeWriteMarker{errs: append([]error(nil), tt.markerErrs...)}
			err := MarkProjectionWriteStarted(context.Background(), marker, writeMarkerTestWork(), instruments, nil)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("MarkProjectionWriteStarted() error = %v, want nil", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("MarkProjectionWriteStarted() error = %v, want %v", err, tt.wantErr)
			}

			got := collectWriteMarkerMetrics(t, reader)
			assertOutcomeCounts(t, "write_marker_deferrals_total", got.deferrals, tt.wantDeferrals)
			assertOutcomeCounts(t, "write_marker_wait_seconds count", got.waits, tt.wantWaits)
		})
	}
}

// TestMarkProjectionWriteStartedRecordsShutdownOutcome proves a shutdown
// during the wait is counted as shutdown, not as an exhausted bound or a
// failed marker.
func TestMarkProjectionWriteStartedRecordsShutdownOutcome(t *testing.T) {
	t.Parallel()

	reader, instruments := newAckWaitReader(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	marker := &fakeWriteMarker{errs: []error{fmt.Errorf("lock timeout: %w", failure.ErrWorkWriteMarkerDeferred)}}

	err := MarkProjectionWriteStarted(ctx, marker, writeMarkerTestWork(), instruments, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("MarkProjectionWriteStarted() error = %v, want context.Canceled", err)
	}
	got := collectWriteMarkerMetrics(t, reader)
	assertOutcomeCounts(t, "write_marker_deferrals_total", got.deferrals, map[string]int64{"shutdown": 1})
	assertOutcomeCounts(t, "write_marker_wait_seconds count", got.waits, map[string]uint64{"shutdown": 1})
}

// TestMarkProjectionWriteStartedAllowsNilInstruments keeps telemetry
// optional, matching Service.Instruments.
func TestMarkProjectionWriteStartedAllowsNilInstruments(t *testing.T) {
	t.Parallel()

	marker := &fakeWriteMarker{errs: []error{fmt.Errorf("lock timeout: %w", failure.ErrWorkWriteMarkerDeferred)}}
	if err := MarkProjectionWriteStarted(context.Background(), marker, writeMarkerTestWork(), nil, nil); err != nil {
		t.Fatalf("MarkProjectionWriteStarted() error = %v, want nil", err)
	}
}

// TestServiceRunRecordsWriteMarkerWaitWithServiceInstruments proves the
// projector service hands its instruments to the write-marker wait, so a
// busy-generation wait is visible on the service's meter.
func TestServiceRunRecordsWriteMarkerWaitWithServiceInstruments(t *testing.T) {
	t.Parallel()

	reader, instruments := newAckWaitReader(t)
	deferred := fmt.Errorf("lock timeout: %w", failure.ErrWorkWriteMarkerDeferred)
	service := writeMarkerTestService(
		&fakeWriteMarker{errs: []error{deferred, deferred}},
		&stubFactStore{}, &stubProjectionRunner{}, &stubProjectorWorkSink{},
	)
	service.Instruments = instruments

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	got := collectWriteMarkerMetrics(t, reader)
	assertOutcomeCounts(t, "write_marker_deferrals_total", got.deferrals, map[string]int64{"retried": 2})
	assertOutcomeCounts(t, "write_marker_wait_seconds count", got.waits, map[string]uint64{"written": 1})
}
