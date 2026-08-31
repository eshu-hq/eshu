// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package querycontract defines the dependency-neutral contracts shared by query families.
//
// It owns response envelopes, profile and capability gates, freshness metadata,
// read ports, the content models those ports exchange, the graph row-value
// decoders every read path uses to pull typed columns out of a driver's
// map[string]any, and the per-route handler span. Family packages can depend on
// these contracts without importing the root query router. Capability
// registration preserves the established profile ceilings, ordered catalog,
// and unknown-capability panic.
//
// StartHandlerSpanWith takes its tracer as an argument so a caller keeping its
// own swappable tracer var, as package query does for its span tests, stays in
// control of which tracer the span lands on.
package querycontract
