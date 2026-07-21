// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package environment provides the canonical environment-alias contract for the
// Eshu platform.
//
// The package centralizes environment naming: Normalize is trim+lowercase,
// Canonical adds alias resolution (production→prod, staging→stage,
// development→dev), and IsKnownToken covers the 12-token union for
// artifact-path detection. EvidenceClass and State provide closed vocabularies
// for evidence provenance and environment binding.
//
// Unknown values pass through normalized and are never rejected or invented.
// This package is the single source of truth; all other packages derive
// environment naming from it.
package environment
