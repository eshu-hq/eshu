// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package impact_test wires the production seam backends for the moved
// handler tests. These tests ran inside package query at base, where the
// query root's init had already assigned impact.DefaultCodeSurface,
// DefaultTraceContext, and DefaultPathProbe. This package cannot import the
// root from its internal test files (compare.go and family_impact_shim.go
// import impact/, so that would cycle), but an external test package may:
// nothing imports impact_test, so wiring the exported root constructors
// here reproduces the base environment exactly with no behavior change.
// Tests that need narrower behavior keep injecting per-handler fakes, per
// the backend fields' contract. See #6060.
package impact_test

import (
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/impact"
)

func init() {
	impact.DefaultCodeSurface = query.NewChangeSurfaceCodeBackend()
	impact.DefaultTraceContext = query.NewDeploymentTraceContext()
	impact.DefaultPathProbe = query.NewImpactPathProbeBackend()
}
