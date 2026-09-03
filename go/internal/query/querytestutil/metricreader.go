// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// WithPackageMetricReader installs a process-global manual-reader meter
// provider for a test and returns the reader, so lazily registered
// query-package instruments (each guarded by its own sync.Once, mirroring
// initImageQueryInstruments in package query's images_telemetry.go and
// initTagHistoryQueryInstruments in its tag_history_telemetry.go) observe only
// this test's datapoints.
//
// It lives here rather than in a _test.go file because packages on both sides
// of the #6060 handler-family split install a reader this way, and Go never
// compiles a package's _test.go files into anything another package can
// import. Root package query keeps a thin wrapper under the old name. reset must zero the caller's package-level
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
// initImageQueryInstruments' doc comment in package query). Without the throwaway call, a
// test is defended only by being first among the package's test files to
// install a provider — an accident of file ordering, not a property of the
// code under test. Burning the delegate-once here first guarantees any
// package-var-cached meter binds to the throwaway, never to this test's
// reader, so a handler that wrongly caches its meter at init time fails
// deterministically instead of passing by file-order luck.
//
// Capturing previous before the throwaway install (not after) matters for
// cleanup: capturing it after would restore the throwaway instead of
// whatever provider genuinely preceded this test, discarding that real prior
// state for whatever test runs next. This ordering does not avoid a
// reader-less restored proxy -- the process-global default delegating proxy
// burns its delegate-once on the throwaway's SetMeterProvider call, so the
// proxy keeps delegating to the throwaway even after this cleanup calls
// SetMeterProvider(previous) again -- it only avoids losing track of what
// "previous" actually was.
//
// Callers must not call t.Parallel(): this installs a process-global OTel
// meter provider and zeroes package-level sync.Once/instrument vars, so a
// caller running concurrently with another test through this same helper (or
// through any test that resolves a package-var-cached meter) would race on
// shared process state, mirroring the "not parallel" note on
// TestRequestMetricsMiddlewareEmitsPerEndpointMetrics in
// package query's request_metrics_test.go.
func WithPackageMetricReader(t *testing.T, reset func()) *sdkmetric.ManualReader {
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
		// throwaway deliberately gets no Shutdown call: sdkmetric
		// .NewMeterProvider() with no WithReader option registers no reader,
		// and only NewPeriodicReader starts a collection goroutine, so there
		// is no goroutine, timer, or exporter connection to release.
		//
		// "No reader" is not "unused". throwaway owns the delegate burned
		// above, so a handler that wrongly caches its meter in a package var
		// binds to throwaway and records its datapoints there -- which is why
		// they never reach this test's reader, and why the test fails. Leaving
		// it un-shut-down keeps that binding honest rather than routing the
		// bad handler's records through a provider that has been torn down.
	})
	return reader
}
