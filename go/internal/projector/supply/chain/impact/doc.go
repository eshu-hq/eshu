// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package impact builds the supply_chain_impact reducer intent
// from one immutable scope generation. The trigger fires on the earliest
// accepted fact across twelve candidate kinds, in original generation
// order: vulnerability CVE, affected-package, EPSS-score, known-exploited,
// and suppression facts, a provider security-alert fact, package-registry
// package identity, an SBOM component, and OCI manifest/index/tag-
// observation/referrer facts. When none of those is present, a git
// generation (one holding a git repository fact) enqueues one marker intent
// instead, anchored on its earliest dependency row (a content_entity Variable
// whose config_kind is dependency) or on its repository fact, carrying
// payload trigger=manifest_dependency (#9874): the reducer then reopens the
// scopes holding advisory evidence for the changed packages rather than
// running a findings pass. Only that marker predicate reads payload keys, and
// only from the already-decoded map; schema-version admission stays with
// root projection. The reducer's DomainSupplyChainImpact handler owns the
// cross-source vulnerability-to-package-to-deployment join; this package
// only selects the trigger fact, a short human-readable reason, and the
// source-system label. The intent's source-system label is the shared
// two-tier projectorintent.SourceSystem fallback (trimmed
// SourceRef.SourceSystem, else trimmed CollectorKind) — the pre-extraction
// local helper had the identical body, so this is a behavior-preserving
// substitution, not a change. Root projector assembly owns lookup
// construction and lifetime, invocation order, queue writes, retries, and
// telemetry.
package impact
