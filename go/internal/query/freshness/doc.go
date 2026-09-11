// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package freshness implements the bounded freshness-drilldown routes (#6642,
// split off the #6060 lane A query-root restructure): Handler, its Mount
// method, the generation lifecycle drilldown
// (GET /api/v0/freshness/generations), the changed-since delta summary
// (GET /api/v0/freshness/changed-since), the service-scope changed-since delta
// summary (GET /api/v0/freshness/services/changed-since, #1943), and the
// freshness-causality read model's closed cause enumeration and generation/
// pending-projection/transition types (Cause, Causality, CauseStatus,
// Generations, PendingProjection, Transition).
//
// Every route gates its read on its own capability
// (ChangedSinceCapability, GenerationLifecycleCapability,
// ServiceChangedSinceCapability -- querycontract.CapabilityUnsupported)
// before touching its reader port, resolves the caller's repository/scope
// grant through querycontract.RepositoryAccessFilterFromContext before
// binding it into the query, and reports a bounded, deterministically
// ordered, truncation-aware result. All three reads are bounded local-host
// Postgres reads implemented by the status store and never require the graph
// backend, so every capability is exact at every profile.
//
// This package imports querycontract (profiles, envelopes, capability
// registration, HTTP helpers, RepositoryAccessFilterFromContext), queryauth
// (AuthContext, only in tests), queryspan (the shared handler-span seam),
// service (CatalogCorrelationStore/Filter/Row, the service-catalog
// correlation read model the service-changed-since route's grant binds
// against), and status (the Postgres-status-store filter/summary/page
// shapes each reader port exchanges); it MUST NOT import the query root, or
// root would cycle back through its own compatibility aliases in
// freshness_alias.go, which import this package for the type aliases and
// forwarders that root and the cmd/api and cmd/mcp-server wiring still use. See
// README.md for the file layout and move evidence, and AGENTS.md for the
// per-symbol export rationale.
package freshness
