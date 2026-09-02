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
//
// It also owns six handler seams promoted out of root for #6060, so a future
// family package can reach them without an import cycle. The
// visualization-packet contract covers VisualizationPacket and its node, edge,
// limits and truncation types, plus VisualizationBuilder with
// NewVisualizationBuilder, AddNode, AddEdge, SetTruth, Empty, EdgeCount and
// Finalize, the stable VisualizationNodeID and VisualizationEdgeID hashers, and
// UnsupportedVisualizationPacket. EvidenceCitationHandle and its dedup key are
// the citation handles those packets carry. Entity-name search covers
// EntityNameSearch, EntityNameMatch, EntityNameScope, the EntityNameSearcher
// port, its limit constants, and the sentinel errors
// ErrEntityNameSearchUnavailable and ErrGlobalGraphEntitySearchUnsupported.
// Content-index readiness covers ErrContentSubstringIndexesNotReady and
// WriteContentSubstringIndexUnavailable. The language taxonomy covers
// CanonicalLanguage, NormalizedLanguageVariants and CoverageLanguageMaps over
// an unexported alias table; the accepted-language set stays in root.
// ClearResolvedEntityRepoProjectionPlaceholders is the #6408 workaround that
// blanks a projection expression a backend returned as literal text.
//
// The sentinel errors are the reason several of these moved rather than being
// copied: root compares them with errors.Is, so both sides have to resolve to
// the same value. A re-declared error would compile and compare false.
package querycontract
