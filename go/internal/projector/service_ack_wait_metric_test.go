// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ackWaitMetrics holds the Ack-wait counter values and histogram counts,
// keyed by outcome.
type ackWaitMetrics struct {
	deferrals map[string]int64
	waits     map[string]uint64
}

func newAckWaitReader(t *testing.T) (*sdkmetric.ManualReader, *telemetry.Instruments) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return reader, instruments
}

func collectAckWaitMetrics(t *testing.T, reader *sdkmetric.ManualReader) ackWaitMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := ackWaitMetrics{deferrals: map[string]int64{}, waits: map[string]uint64{}}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "eshu_dp_projector_ack_deferrals_total":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s data = %T, want Sum[int64]", m.Name, m.Data)
				}
				for _, point := range sum.DataPoints {
					outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
					got.deferrals[outcome.AsString()] += point.Value
				}
			case "eshu_dp_projector_ack_wait_seconds":
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

func assertOutcomeCounts[V int64 | uint64](t *testing.T, name string, got, want map[string]V) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for outcome, value := range want {
		if got[outcome] != value {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}

// TestAckWhenScopeFreeRecordsDeferralAndWaitOutcomes proves every deferred Ack
// is counted with what the loop did next, and every Ack that waited records
// one wait-duration point with its terminal outcome. An Ack that never waited
// records nothing, so the histogram measures only busy-scope waits.
func TestAckWhenScopeFreeRecordsDeferralAndWaitOutcomes(t *testing.T) {
	t.Parallel()

	deferred := fmt.Errorf("lock timeout: %w", failure.ErrWorkAckDeferred)
	ackFailure := errors.New("connection reset")
	renewFailure := errors.New("heartbeat: connection reset")
	renewOK := heartbeaterFunc(func(context.Context, ScopeGenerationWork) error { return nil })
	tests := []struct {
		name          string
		ackErrs       []error
		heartbeater   ProjectorWorkHeartbeater
		maxRetries    int
		wantErr       error
		wantDeferrals map[string]int64
		wantWaits     map[string]uint64
	}{
		{
			name:          "no deferral records nothing",
			ackErrs:       []error{nil},
			heartbeater:   renewOK,
			wantDeferrals: map[string]int64{},
			wantWaits:     map[string]uint64{},
		},
		{
			name:          "deferred then succeeded",
			ackErrs:       []error{deferred, deferred, nil},
			heartbeater:   renewOK,
			wantDeferrals: map[string]int64{"retried": 2},
			wantWaits:     map[string]uint64{"succeeded": 1},
		},
		{
			name:          "bound exhausted abandons",
			ackErrs:       []error{deferred, deferred, deferred},
			heartbeater:   renewOK,
			maxRetries:    3,
			wantErr:       failure.ErrWorkAckDeferred,
			wantDeferrals: map[string]int64{"retried": 2, "abandoned": 1},
			wantWaits:     map[string]uint64{"abandoned": 1},
		},
		{
			name:    "renewal reports superseded",
			ackErrs: []error{deferred},
			heartbeater: heartbeaterFunc(func(context.Context, ScopeGenerationWork) error {
				return fmt.Errorf("renew: %w", failure.ErrWorkSuperseded)
			}),
			wantErr:       failure.ErrWorkSuperseded,
			wantDeferrals: map[string]int64{"retried": 1},
			wantWaits:     map[string]uint64{"superseded": 1},
		},
		{
			name:    "renewal reports claim lost",
			ackErrs: []error{deferred},
			heartbeater: heartbeaterFunc(func(context.Context, ScopeGenerationWork) error {
				return fmt.Errorf("renew: %w", failure.ErrWorkClaimLost)
			}),
			wantErr:       failure.ErrWorkClaimLost,
			wantDeferrals: map[string]int64{"retried": 1},
			wantWaits:     map[string]uint64{"claim_lost": 1},
		},
		{
			name:    "renewal returns an error",
			ackErrs: []error{deferred},
			heartbeater: heartbeaterFunc(func(context.Context, ScopeGenerationWork) error {
				return renewFailure
			}),
			wantErr:       renewFailure,
			wantDeferrals: map[string]int64{"retried": 1},
			wantWaits:     map[string]uint64{"failed": 1},
		},
		{
			name:          "ack error after waiting fails",
			ackErrs:       []error{deferred, ackFailure},
			heartbeater:   renewOK,
			wantErr:       ackFailure,
			wantDeferrals: map[string]int64{"retried": 1},
			wantWaits:     map[string]uint64{"failed": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader, instruments := newAckWaitReader(t)
			sink := &sequencedAckSink{ackErrs: tt.ackErrs}
			err := AckWhenScopeFree(
				context.Background(), sink, tt.heartbeater, instruments,
				ScopeGenerationWork{}, runtime.Result{}, tt.maxRetries, nil,
			)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("AckWhenScopeFree() error = %v, want nil", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("AckWhenScopeFree() error = %v, want %v", err, tt.wantErr)
			}

			got := collectAckWaitMetrics(t, reader)
			assertOutcomeCounts(t, "ack_deferrals_total", got.deferrals, tt.wantDeferrals)
			assertOutcomeCounts(t, "ack_wait_seconds count", got.waits, tt.wantWaits)
		})
	}
}

// TestAckWhenScopeFreeRecordsShutdownOutcome proves a shutdown during the wait
// is counted as shutdown, not as an abandoned bound or a failed Ack.
func TestAckWhenScopeFreeRecordsShutdownOutcome(t *testing.T) {
	t.Parallel()

	reader, instruments := newAckWaitReader(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink := &sequencedAckSink{ackErrs: []error{fmt.Errorf("lock timeout: %w", failure.ErrWorkAckDeferred)}}

	err := AckWhenScopeFree(ctx, sink, nil, instruments, ScopeGenerationWork{}, runtime.Result{}, 0, nil)
	if !errors.Is(err, failure.ErrWorkAckDeferred) {
		t.Fatalf("AckWhenScopeFree() error = %v, want ErrWorkAckDeferred", err)
	}
	got := collectAckWaitMetrics(t, reader)
	assertOutcomeCounts(t, "ack_deferrals_total", got.deferrals, map[string]int64{"shutdown": 1})
	assertOutcomeCounts(t, "ack_wait_seconds count", got.waits, map[string]uint64{"shutdown": 1})
}

// TestAckWhenScopeFreeCountsRetryBeforeShutdownRenewal pins the documented
// split: a deferral followed by a renewal that shutdown interrupts counts
// retried (what the loop decided at the deferral), while the wait histogram
// records how the wait ended.
func TestAckWhenScopeFreeCountsRetryBeforeShutdownRenewal(t *testing.T) {
	t.Parallel()

	reader, instruments := newAckWaitReader(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &sequencedAckSink{ackErrs: []error{fmt.Errorf("lock timeout: %w", failure.ErrWorkAckDeferred)}}
	renewal := heartbeaterFunc(func(ctx context.Context, _ ScopeGenerationWork) error {
		cancel()
		return fmt.Errorf("heartbeat projector work: %w", ctx.Err())
	})

	err := AckWhenScopeFree(ctx, sink, renewal, instruments, ScopeGenerationWork{}, runtime.Result{}, 0, nil)
	if !errors.Is(err, failure.ErrWorkAckDeferred) {
		t.Fatalf("AckWhenScopeFree() error = %v, want ErrWorkAckDeferred", err)
	}
	got := collectAckWaitMetrics(t, reader)
	assertOutcomeCounts(t, "ack_deferrals_total", got.deferrals, map[string]int64{"retried": 1})
	assertOutcomeCounts(t, "ack_wait_seconds count", got.waits, map[string]uint64{"shutdown": 1})
}

// TestAckWhenScopeFreeAllowsNilInstruments keeps telemetry optional, matching
// Service.Instruments.
func TestAckWhenScopeFreeAllowsNilInstruments(t *testing.T) {
	t.Parallel()

	sink := &sequencedAckSink{ackErrs: []error{fmt.Errorf("lock timeout: %w", failure.ErrWorkAckDeferred), nil}}
	if err := AckWhenScopeFree(context.Background(), sink, nil, nil, ScopeGenerationWork{}, runtime.Result{}, 0, nil); err != nil {
		t.Fatalf("AckWhenScopeFree() error = %v, want nil", err)
	}
}

// TestServiceRunRecordsAckWaitWithServiceInstruments proves the projector
// service hands its instruments to the Ack wait, so an abandoned busy-scope
// wait is visible on the service's meter.
func TestServiceRunRecordsAckWaitWithServiceInstruments(t *testing.T) {
	t.Parallel()

	reader, instruments := newAckWaitReader(t)
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource: &stubProjectorWorkSource{workItems: []ScopeGenerationWork{{
			Scope:        scope.IngestionScope{ScopeID: "scope-123", ScopeKind: scope.KindRepository},
			Generation:   scope.ScopeGeneration{ScopeID: "scope-123", GenerationID: "generation-1"},
			AttemptCount: 1,
		}}},
		FactStore:         &stubFactStore{},
		Runner:            &stubProjectionRunner{},
		WorkSink:          &alwaysDeferSink{},
		Heartbeater:       &stubProjectorWorkHeartbeater{},
		HeartbeatInterval: time.Hour,
		Instruments:       instruments,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	got := collectAckWaitMetrics(t, reader)
	assertOutcomeCounts(t, "ack_deferrals_total", got.deferrals, map[string]int64{
		"retried":   DefaultAckWaitMaxRetries - 1,
		"abandoned": 1,
	})
	assertOutcomeCounts(t, "ack_wait_seconds count", got.waits, map[string]uint64{"abandoned": 1})
}
