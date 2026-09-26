// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package changedsince holds the changed-since delta contract: the bounded,
// closed-vocabulary diff of one prior generation's fact set against the
// current active generation's fact set, for both the repository-scope and
// service-scope surfaces. The root internal/status package's doc.go
// documents this contract in prose, but neither RawSnapshot nor Report
// aggregates it — the only real consumer is internal/query/freshness, which
// imports this package directly to build the changed-since query handlers.
// Root and every sibling leaf may still import changedsince; this package
// must never import the root or a sibling leaf.
//
// Filter, Summary, CategoryDelta, and the Classification/Category enums
// define the bounded repository-scope contract: a diff of one prior
// generation's fact set against the current active generation's fact set,
// grouped into evidence categories (files, content entities, facts) and the
// closed verdict set (added, updated, unchanged, retired, superseded).
// Counts are exact; Filter clamps the per-classification sample handles to
// MaxSampleLimit. Summary's Unavailable flag distinguishes a scope with no
// current active generation from a genuinely empty delta so the surface
// never reports all-unchanged when it cannot diff. UnavailableReason is
// populated for fail-closed cases such as generation history pruned by
// retention.
//
// ServiceFilter and ServiceSummary define the service-scope variant (#1943):
// the same verdict set, counts, sample, and Unavailable shapes, but keyed by
// service_id and diffed over a per-service materialization generation
// lineage instead of an ingestion scope. It reports the ownership
// (CategoryOwnership, #1943), deployment (CategoryDeployment, #1985),
// runtime (CategoryRuntime, #1986), dependencies (CategoryDependencies,
// #1987), docs (CategoryDocs, #1988), and incidents (CategoryIncidents,
// #1989) evidence families; the remaining family (vulnerabilities) appends
// to ServiceCategories as it lands.
//
// A service id holds one lineage per ingestion scope (#6475), so
// ServiceFilter also carries an optional ScopeID selector and the caller's
// grant (Scoped, AllowedRepositoryIDs, AllowedScopeIDs), which the reader
// binds on each lineage row's scope_id. When more than one admitted scope
// holds a lineage and no ScopeID was given, ServiceSummary lists them in
// AmbiguousScopeIDs (bounded by MaxServiceScopeCandidates) instead of a diff.
//
// Timestamp formats a database timestamp as RFC3339 UTC, or the empty
// string for a zero value — the same shape the generation lifecycle
// drilldown's own timestamp helper promises to keep in lockstep.
package changedsince
