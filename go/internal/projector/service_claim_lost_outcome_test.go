// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestServiceRunDoesNotCountFailedOutcomeForLostClaim proves a projection
// failure whose Fail is rejected as a lost claim records no failed outcome:
// the owning attempt records the real one, and a stale failed point would
// skew the projection success SLO.
func TestServiceRunDoesNotCountFailedOutcomeForLostClaim(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource: &stubProjectorWorkSource{workItems: []ScopeGenerationWork{{
			Scope:        scope.IngestionScope{ScopeID: "scope-123", ScopeKind: scope.KindRepository},
			Generation:   scope.ScopeGeneration{ScopeID: "scope-123", GenerationID: "generation-1"},
			AttemptCount: 1,
		}}},
		FactStore:   &stubFactStore{},
		Runner:      &stubProjectionRunner{runErr: errors.New("projection failed")},
		WorkSink:    &stubProjectorWorkSink{failErr: fmt.Errorf("stale attempt: %w", failure.ErrWorkClaimLost)},
		Instruments: instruments,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
		Wait:        func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_projections_completed_total" {
				continue
			}
			sum, _ := m.Data.(metricdata.Sum[int64])
			for _, point := range sum.DataPoints {
				if status, ok := point.Attributes.Value("status"); ok && status.AsString() == "failed" {
					t.Fatalf("projections_completed{status=failed} = %d, want no point for a lost claim", point.Value)
				}
			}
		}
	}
	if out := logs.String(); strings.Contains(out, `"msg":"projection failed"`) {
		t.Fatalf("logged a failed projection for a lost claim:\n%s", out)
	}
	if out := logs.String(); !strings.Contains(out, "projector work claim lost to another attempt") {
		t.Fatalf("missing claim-lost log:\n%s", out)
	}
}
