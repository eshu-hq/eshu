// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type staticReducerGraphDrain struct {
	uncommittedCanonical bool
	err                  error
}

func (s staticReducerGraphDrain) HasUncommittedCanonicalCodeScopes(context.Context) (bool, error) {
	return s.uncommittedCanonical, s.err
}

func TestRepoDependencyProjectionRunnerWaitsForCanonicalCodeQuiescence(t *testing.T) {
	t.Parallel()

	reader := &fakeRepoDependencyIntentStore{leaseGranted: true}
	runner := RepoDependencyProjectionRunner{
		IntentReader:        reader,
		LeaseManager:        reader,
		AcceptanceUnitGate:  reader,
		CanonicalQuiescence: staticReducerGraphDrain{uncommittedCanonical: true},
		Config:              RepoDependencyProjectionRunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if result.BlockedReadiness != 1 {
		t.Fatalf("BlockedReadiness = %d, want 1", result.BlockedReadiness)
	}
	if got := reader.claimCount(); got != 0 {
		t.Fatalf("lease claims = %d, want 0 while canonical code scopes are uncommitted", got)
	}
}

func TestRepoDependencyProjectionRunnerRecordsQuiescenceBlockedCycle(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	intentStore := &fakeRepoDependencyIntentStore{leaseGranted: true}
	runner := RepoDependencyProjectionRunner{
		Instruments:         instruments,
		IntentReader:        intentStore,
		LeaseManager:        intentStore,
		AcceptanceUnitGate:  intentStore,
		CanonicalQuiescence: staticReducerGraphDrain{uncommittedCanonical: true},
		Config:              RepoDependencyProjectionRunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if result.BlockedReadiness != 1 {
		t.Fatalf("BlockedReadiness = %d, want 1", result.BlockedReadiness)
	}

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resources); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !quiescenceBlockedDurationHasPoint(resources) {
		t.Fatal("blocked cycle left no canonical-write-duration point for domain repo_dependency")
	}
}

func quiescenceBlockedDurationHasPoint(resources metricdata.ResourceMetrics) bool {
	for _, scope := range resources.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != "eshu_dp_canonical_write_duration_seconds" {
				continue
			}
			histogram, ok := candidate.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, point := range histogram.DataPoints {
				domain, domainOK := point.Attributes.Value(attribute.Key("domain"))
				if domainOK && domain.AsString() == DomainRepoDependency && point.Count == 1 {
					return true
				}
			}
		}
	}
	return false
}
