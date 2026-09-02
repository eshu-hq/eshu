// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// withPackageMetricReader installs a process-global manual-reader meter
// provider for a test and returns the reader.
//
// The implementation moved to querytestutil (#6060) so the handler families
// that left this package can install a reader the same way; a _test.go
// declaration is importable from nowhere. See
// querytestutil.WithPackageMetricReader for the delegate-once ordering, the
// cleanup contract, and why callers must not call t.Parallel(). This wrapper
// exists so the callers in this package keep the name they already use.
func withPackageMetricReader(t *testing.T, reset func()) *sdkmetric.ManualReader {
	t.Helper()
	return querytestutil.WithPackageMetricReader(t, reset)
}
