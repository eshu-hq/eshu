// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// connectionReset builds the error net/http returns when the registry drops
// the TCP connection mid-request.
func connectionReset() error {
	return collector.RegistryTransportFailure("oci", "", "ping", &url.Error{
		Op: "Get", URL: "https://registry.example.test/v2/",
		Err: &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)},
	})
}

type pingFailingClient struct {
	stubRegistryClient
	pingErr error
}

func (c *pingFailingClient) Ping(context.Context) error { return c.pingErr }

func transportTestSource(t *testing.T, clients map[string]RegistryClient, instruments *telemetry.Instruments) *Source {
	t.Helper()
	return &Source{
		Config: Config{
			CollectorInstanceID: "oci-runtime-test",
			Targets: []TargetConfig{
				{Provider: ociregistry.ProviderGHCR, Registry: "ghcr.io", Repository: "team/first", TagLimit: 1},
				{Provider: ociregistry.ProviderGHCR, Registry: "ghcr.io", Repository: "team/second", TagLimit: 1},
			},
		},
		ClientFactory: ClientFactoryFunc(func(_ context.Context, target TargetConfig) (RegistryClient, error) {
			return clients[target.Repository], nil
		}),
		Instruments: instruments,
	}
}

func TestSourceNextIsolatesTransientTransportFailureOnPing(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	healthy := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			Digest: testManifestDigest, MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body: testManifestBody(t), SizeBytes: 512,
		},
	}
	source := transportTestSource(t, map[string]RegistryClient{
		"team/first":  &pingFailingClient{pingErr: connectionReset()},
		"team/second": healthy,
	}, instruments)

	_, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil: a transient transport failure must not exit the collector", err)
	}
	if ok {
		t.Fatal("Next() ok = true, want false for the target that hit a transport error")
	}

	// The failed target is skipped for this cycle; the next target still scans.
	collected, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("second Next() = ok %v err %v, want the healthy target scanned", ok, err)
	}
	if collected.Scope.ScopeID == "" {
		t.Fatal("second Next() returned an empty scope")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := scanResultCount(rm, "retryable_transport"); got != 1 {
		t.Fatalf("scan duration samples with result=retryable_transport = %d, want 1", got)
	}
}

func TestSourceNextKeepsCancellationAndNonTransportFailuresFatal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pingErr error
		cancel  bool
	}{
		{
			name: "certificate failure is misconfiguration not transport",
			pingErr: collector.RegistryTransportFailure("oci", "", "ping", &url.Error{
				Op: "Get", Err: errors.New("x509: certificate signed by unknown authority"),
			}),
		},
		{
			name:    "cancelled context",
			pingErr: connectionReset(),
			cancel:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := transportTestSource(t, map[string]RegistryClient{
				"team/first": &pingFailingClient{pingErr: tt.pingErr},
			}, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			if _, _, err := source.Next(ctx); err == nil {
				t.Fatal("Next() error = nil, want the failure propagated")
			}
		})
	}
}

func scanResultCount(rm metricdata.ResourceMetrics, result string) uint64 {
	var count uint64
	for _, scoped := range rm.ScopeMetrics {
		for _, m := range scoped.Metrics {
			if m.Name != "eshu_dp_oci_registry_scan_duration_seconds" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, point := range hist.DataPoints {
				if value, ok := point.Attributes.Value(telemetry.AttrResult(result).Key); ok && value.AsString() == result {
					count += point.Count
				}
			}
		}
	}
	return count
}
