// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package visualization projects executed read-only Cypher result rows
// into bounded, renderable visualization packets. It split out of
// package codequery (#6060 naming follow-up) so the projector can be
// probed, tested, and changed without pulling in the rest of the code
// surface, and so it can one day move into its own repo.
//
// The packet is a pure transformation of rows the authorized query
// already returned: it performs no further graph access, and scalar
// columns yield an explicit unsupported packet rather than a
// fabricated subgraph.
//
// Import discipline: this package may import the same
// dependency-neutral leaves codequery uses (querycontract) and the
// Neo4j driver types for row projection, but NEVER package codequery
// itself and NEVER root package query -- both would create an import
// cycle (codequery calls this leaf, and root aliases codequery).
package visualization
