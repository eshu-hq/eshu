// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// withPackageMetricReader installs a process-global manual-reader meter
// provider for a test and returns the reader, so lazily registered
// query-package instruments (each guarded by its own sync.Once, mirroring
// initImageQueryInstruments in images_telemetry.go and
// initTagHistoryQueryInstruments in tag_history_telemetry.go) observe only
// this test's datapoints. reset must zero the caller's package-level
// sync.Once and instrument vars so registration re-runs against the new
// provider; a test with no such instruments to reset may pass a no-op func.
//
// previous is captured before installing anything else, then the delegate-once
// on the OTel global proxy is burned on a throwaway provider before the
// reader's own provider goes live. otel.Meter() inside a sync.Once resolves
// its meter from whichever provider is current at the moment that Once
// fires; if some other test file's sync.Once fires first in this process and
// its own SetMeterProvider call is the very first one ever made, the OTel
// global proxy binds that meter's delegate permanently to it (see
// initImageQueryInstruments' doc comment). Without the throwaway call, a
// test is defended only by being first among the package's test files to
// install a provider — an accident of file ordering, not a property of the
// code under test. Burning the delegate-once here first guarantees any
// package-var-cached meter binds to the throwaway, never to this test's
// reader, so a handler that wrongly caches its meter at init time fails
// deterministically instead of passing by file-order luck.
//
// Capturing previous before the throwaway install (not after) matters for
// cleanup: capturing it after would restore the throwaway itself, leaving
// the process-global provider reader-less for whatever test runs next.
//
// Callers must not call t.Parallel(): this installs a process-global OTel
// meter provider and zeroes package-level sync.Once/instrument vars, so a
// caller running concurrently with another test through this same helper (or
// through any test that resolves a package-var-cached meter) would race on
// shared process state, mirroring the "not parallel" note on
// TestRequestMetricsMiddlewareEmitsPerEndpointMetrics in
// request_metrics_test.go.
func withPackageMetricReader(t *testing.T, reset func()) *sdkmetric.ManualReader {
	t.Helper()
	previous := otel.GetMeterProvider()

	throwaway := sdkmetric.NewMeterProvider()
	otel.SetMeterProvider(throwaway)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	reset()

	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		reset()
		_ = provider.Shutdown(context.Background())
		_ = throwaway.Shutdown(context.Background())
	})
	return reader
}
