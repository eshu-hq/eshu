// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// retryableStatus builds the error the distribution client returns for an
// HTTP status failure: a bounded RegistryFailure classified by status with
// the SDK HTTPError carrying the status code.
func retryableStatus(t *testing.T, status int) error {
	t.Helper()
	return collector.RegistryHTTPFailure("oci", "", "ping", status, sdk.HTTPError{
		Provider:   "oci",
		StatusCode: status,
		Message:    http.StatusText(status),
	})
}

func TestSourceNextSkipsRetryableHTTPStatusOnPing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
	}{
		{name: "server error skips the target", status: http.StatusServiceUnavailable},
		{name: "rate limited skips the target", status: http.StatusTooManyRequests},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				"team/first":  &pingFailingClient{pingErr: retryableStatus(t, tt.status)},
				"team/second": healthy,
			}, instruments)

			// The failed target is skipped for this cycle and the healthy
			// target scans in the same call.
			collected, ok, err := source.Next(context.Background())
			if err != nil {
				t.Fatalf("Next() error = %v, want nil: an HTTP %d must not exit the collector", err, tt.status)
			}
			if !ok {
				t.Fatal("Next() ok = false, want the healthy target scanned after the status failure was skipped")
			}
			if collected.Scope.ScopeID == "" {
				t.Fatal("Next() returned an empty scope")
			}

			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if got := scanResultCount(rm, "retryable_status"); got != 1 {
				t.Fatalf("scan duration samples with result=retryable_status = %d, want 1", got)
			}
		})
	}
}

func TestSourceNextEscalatesPersistentRetryableStatusToFatal(t *testing.T) {
	t.Parallel()

	source := transportTestSource(t, map[string]RegistryClient{
		"team/first": &pingFailingClient{pingErr: retryableStatus(t, http.StatusServiceUnavailable)},
	}, nil)
	source.Config.Targets = source.Config.Targets[:1]

	limit := sdk.MaxConsecutiveTransportFailures
	for attempt := 1; attempt < limit; attempt++ {
		if _, ok, err := source.Next(context.Background()); err != nil || ok {
			t.Fatalf("attempt %d: Next() = ok %v err %v, want a skipped idle poll below the ceiling", attempt, ok, err)
		}
	}
	_, ok, err := source.Next(context.Background())
	if err == nil || ok {
		t.Fatalf("attempt %d: Next() = ok %v err %v, want a fatal error at the consecutive-failure ceiling", limit, ok, err)
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("Next() error = %q, want it to name the consecutive-failure ceiling", err)
	}
}

func TestSourceNextKeepsTerminalHTTPStatusFatal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		cancel bool
	}{
		{name: "bad request is terminal", status: http.StatusBadRequest},
		{name: "unauthorized is auth denial", status: http.StatusUnauthorized},
		{name: "not found is terminal", status: http.StatusNotFound},
		{name: "cancelled context stays fatal on retryable status", status: http.StatusServiceUnavailable, cancel: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := transportTestSource(t, map[string]RegistryClient{
				"team/first": &pingFailingClient{pingErr: retryableStatus(t, tt.status)},
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

func TestSourceNextRetryableStatusLogCarriesFailureClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		causeClass string
	}{
		{name: "server error", status: http.StatusServiceUnavailable, causeClass: collector.RegistryFailureRetryable},
		{name: "rate limited", status: http.StatusTooManyRequests, causeClass: collector.RegistryFailureRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			source := transportTestSource(t, map[string]RegistryClient{
				"team/first": &pingFailingClient{pingErr: retryableStatus(t, tt.status)},
			}, nil)
			source.Config.Targets = source.Config.Targets[:1]
			source.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

			if _, _, err := source.Next(context.Background()); err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			out := logs.String()
			if want := `"cause_class":"` + tt.causeClass + `"`; !strings.Contains(out, want) {
				t.Fatalf("transient log missing %s:\n%s", want, out)
			}
		})
	}
}
