// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package querycontract defines the dependency-neutral contracts shared by query families.
//
// It owns response envelopes, profile and capability gates, freshness metadata,
// read ports, the content models those ports exchange, the graph row-value
// decoders every read path uses to pull typed columns out of a driver's
// map[string]any, the collector-list readiness contract that lets a caller tell
// an empty page from a disabled collector, and the scoped-token
// repository-access authorization seam (RepositoryAccessFilter and the
// inline-map grant predicate primitives). Family packages can depend on these
// contracts without importing the root query router. Capability registration
// preserves the established profile ceilings, ordered catalog, and
// unknown-capability panic.
//
// The readiness types and their two Build functions live here; deciding when to
// run the probe and attaching the result to a response body stays in package
// query, because that is request-time orchestration rather than contract.
package querycontract
