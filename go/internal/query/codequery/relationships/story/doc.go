// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package story implements the relationship-story assembly for the
// code-family queries: response shaping, target resolution, the class
// hierarchy and override reads, the graph reads, and the NornicDB
// story readers. It split out of package codequery (#6060 naming
// follow-up) so the story can be probed, tested, and changed without
// pulling in the rest of the code surface, and so it can one day move
// into its own repo. It builds on the parent relationships leaf for
// label resolution, node patterns, row normalization, and the
// inheritance grant filter.
//
// The split keeps the *CodeHandler readers in codequery -- the
// queryplan-pinned story methods carry source_sha256 pins which are
// never re-frozen -- plus the HTTP handlers and the grant plumbing,
// and calls this leaf through same-named forwarders.
//
// Import discipline: this package may import its parent relationships
// leaf and the same dependency-neutral leaves codequery uses
// (querycontract, codemodel, codeshaping) but NEVER package codequery
// itself and NEVER root package query -- both would create an import
// cycle (codequery calls this leaf, and root aliases codequery).
package story
