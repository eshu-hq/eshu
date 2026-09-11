// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relationships implements entity relationship reads for the
// code-family queries: direct and transitive CALLS traversal, the
// NornicDB one-hop/metadata/enrichment reads, entity-label resolution,
// and response shaping. It split out of package codequery (#6060
// naming follow-up) so the reads can be probed, tested, and changed
// without pulling in the rest of the code surface, and so they can one
// day move into their own repo.
//
// The split keeps four *CodeHandler graph-read methods in codequery --
// relationshipsGraphRow, transitiveRelationshipsGraphRow,
// nornicDBTransitiveOneHopRows, and nornicDBRelationshipEntityLabel
// carry queryplan source_sha256 pins in grandfathered_non_hot.go,
// which are never re-frozen -- plus the HTTP handler and the
// content-fallback path, and calls this leaf through same-named
// forwarders. The relationship story family builds on this leaf for
// label resolution, node patterns, and the inheritance grant filter.
//
// Import discipline: this package may import the same dependency-neutral
// leaves codequery uses (querycontract, rows, queryselector)
// but NEVER package codequery itself and NEVER root package query --
// both would create an import cycle (codequery calls this leaf, and
// root aliases codequery).
package relationships
