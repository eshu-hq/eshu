// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package confluence collects read-only Confluence documentation evidence.
//
// The package supports bounded collection by Confluence space or root page
// tree, normalizes visible pages into source-neutral documentation facts, and
// preserves source provenance, freshness, labels, links, ownership hints, and
// partial-sync evidence without mutating Confluence. Callers may attach a
// doctruth.Extractor plus claim hints to emit non-authoritative mention and
// claim-candidate facts from the same page evidence. The HTTP client keeps
// pagination bounded to the configured Confluence base URL and handles next
// links that already include the Atlassian Cloud /wiki context path.
// ListSpacePages and ListPageTree additionally bound the total cursor walk
// by a configurable max-total-pages cap, a defensive fetch-count backstop,
// and repeated-cursor detection, reporting truncation instead of silently
// dropping pages the provider still had to return. Source
// implements collector.ObservedSource and returns collector.CollectorObservation
// so collector.observe telemetry covers real Confluence collection attempts
// without emitting spans for drained polls.
package confluence
