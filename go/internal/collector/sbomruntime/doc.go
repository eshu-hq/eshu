// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbomruntime fetches hosted SBOM and attestation documents for
// claim-driven collector work and converts them into reported-confidence source
// facts.
//
// The runtime owns fetching, source redaction, claim-boundary construction, and
// in-toto statement envelope emission. SBOM parsing stays in
// internal/collector/sbomdocument, and OCI referrer discovery stays in
// internal/collector/ociregistry. Reducers remain the only stage that attaches
// SBOM or attestation facts to image truth.
package sbomruntime
