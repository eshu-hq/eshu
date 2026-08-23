// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package querycontract defines the dependency-neutral contracts shared by query families.
//
// It owns response envelopes, profile and capability gates, freshness metadata,
// read ports, and the content models those ports exchange. Family packages can
// depend on these contracts without importing the root query router. Capability
// registration preserves the established profile ceilings, ordered catalog,
// and unknown-capability panic.
package querycontract
