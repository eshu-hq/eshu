// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this route's capability before any test runs.
//
// In production the registration comes from contract/metrics.go, which
// the root query package links. This package's test binary never links
// root, so without this every handler test would fail the capability gate
// with a 501 that says nothing about the handler. It registers Support(), the
// same declaration production uses. Do not delete it as redundant.
func TestMain(m *testing.M) {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{Capability: Capability, Support: Support()},
	)
	os.Exit(m.Run())
}
