// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychaincore

// DetectionProfile names the evidence tier a supply-chain impact finding
// meets. Reducers emit every finding with a tier so downstream readers can
// choose between low-noise precision and broader recall without losing
// truth labels.
type DetectionProfile string

const (
	// DetectionProfilePrecise marks findings backed by an exact installed
	// version anchor (lockfile, manifest with pinned version, SBOM
	// component version) that resolved with an ecosystem-aware matcher.
	// Range-only manifest, malformed, derived product/CPE, and
	// missing-version evidence do not qualify. Unsupported matcher
	// ecosystems are withheld from impact findings and surfaced through
	// readiness coverage gaps instead.
	DetectionProfilePrecise DetectionProfile = "precise"
	// DetectionProfileComprehensive marks findings that do not meet the
	// precise bar but still carry owned anchor evidence (SBOM component,
	// CPE-derived image path, range-only manifest, malformed range, or
	// missing observed version). They keep their truth labels (status,
	// confidence, runtime_reachability) and explicit missing-evidence reasons
	// so callers can interpret recall correctly.
	DetectionProfileComprehensive DetectionProfile = "comprehensive"
)
