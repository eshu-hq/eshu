// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package extractor holds the shared typed-depth foundation for the GCP cloud
// collector's per-asset-type extractors.
//
// The package owns the extractor registry (RegisterAssetExtractor,
// ExtractAssetAttributes), the extraction input/output shapes (ExtractContext,
// AttributeExtraction), the provider relationship observation shape, the claim
// Boundary and ParentScopeKind, and the small shared helpers extractors use
// (CMEK key normalization, sorted dedup, RFC3339 normalization). It imports
// only the standard library plus go/internal/facts (StableID for
// push-endpoint fingerprints), so both the parent gcpcloud package and every
// extractor group package can depend on it without forming an import cycle.
//
// Registration is init-driven: each extractor group package registers its
// asset types from init functions, and the collector binary imports the group
// packages for their side effects. The registry panics on blank, nil, or
// duplicate registration so wiring mistakes fail at startup, never silently.
package extractor
