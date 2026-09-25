// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// BenchmarkGrantCoverageRecheck measures the per-emission producer-grant
// recheck (Source.validateGrantCoverage) for a granted core-owned kind: the
// hot path that runs on every extension result. The grant set holds fifty
// entries so the scan is representative of a busy registry.
func BenchmarkGrantCoverageRecheck(b *testing.B) {
	manifest, grants := grantedCoreKindManifest()
	for i := 0; i < 49; i++ {
		grants = append(grants, component.ProducerGrant{
			ProducerID:     "dev.example.collector.other",
			Version:        "0.1.0",
			Kind:           "aws_resource",
			SchemaVersions: []string{"1.0.0"},
			Scope:          "repo",
			ExpiresAt:      time.Now().Add(time.Hour).UTC(),
		})
	}
	item := testWorkItem()
	result := grantedCoreKindResult(item)
	source, err := NewSource(benchConfig(manifest, grants))
	if err != nil {
		b.Fatalf("NewSource() error = %v, want nil", err)
	}
	benchRecheck(b, source, result)
}

// BenchmarkGrantCoverageRecheckObserved is the same recheck with the real
// GrantTelemetry adapter (counter, span, log) attached, over a background
// context (no recording span), which is the unsampled hot-path cost.
func BenchmarkGrantCoverageRecheckObserved(b *testing.B) {
	manifest, grants := grantedCoreKindManifest()
	for i := 0; i < 49; i++ {
		grants = append(grants, component.ProducerGrant{
			ProducerID:     "dev.example.collector.other",
			Version:        "0.1.0",
			Kind:           "aws_resource",
			SchemaVersions: []string{"1.0.0"},
			Scope:          "repo",
			ExpiresAt:      time.Now().Add(time.Hour).UTC(),
		})
	}
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	config := benchConfig(manifest, grants)
	config.GrantObserver = NewGrantTelemetry(inst, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	source, err := NewSource(config)
	if err != nil {
		b.Fatalf("NewSource() error = %v, want nil", err)
	}
	benchRecheck(b, source, grantedCoreKindResult(testWorkItem()))
}

func benchConfig(manifest component.Manifest, grants []component.ProducerGrant) Config {
	return Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		Grants:              grants,
	}
}

func benchRecheck(b *testing.B, source *Source, result sdkcollector.Result) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := source.validateGrantCoverage(context.Background(), result); err != nil {
			b.Fatalf("validateGrantCoverage() error = %v, want nil", err)
		}
	}
}
