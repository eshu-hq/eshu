// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supplychainimpact builds the supply_chain_impact reducer intent
// from one immutable scope generation. The trigger fires on the earliest
// accepted fact across twelve candidate kinds, in original generation
// order: vulnerability CVE, affected-package, EPSS-score, known-exploited,
// and suppression facts, a provider security-alert fact, package-registry
// package identity, an SBOM component, and OCI manifest/index/tag-
// observation/referrer facts. Only envelope-level fields are read; no
// payload is decoded, and schema-version admission stays with root
// projection. The reducer's DomainSupplyChainImpact handler owns the
// cross-source vulnerability-to-package-to-deployment join; this package
// only selects the trigger fact, a short human-readable reason, and the
// source-system label. The intent's source-system label is the shared
// two-tier projectorintent.SourceSystem fallback (trimmed
// SourceRef.SourceSystem, else trimmed CollectorKind) — the pre-extraction
// local helper had the identical body, so this is a behavior-preserving
// substitution, not a change. Root projector assembly owns lookup
// construction and lifetime, invocation order, queue writes, retries, and
// telemetry.
package supplychainimpact
