// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package chain implements call-chain traversal for the code-family
// queries: request validation and repository resolution, the
// Neo4j-compat and NornicDB shortestPath builders, and node shaping.
// It split out of package codequery (#6060 naming follow-up) so the
// traversal can be read, tested, and changed without pulling in the
// rest of the code surface, and so it can one day move into its own
// repo.
//
// The split keeps the *CodeHandler methods in codequery/callers.go --
// three carry queryplan source_sha256 pins (handleCallChain in
// query-source-coverage.yaml; nornicDBCallChainOneHopRows and
// callChainCandidateOneHopRows in grandfathered_non_hot.go, which are
// never re-frozen) and call this leaf through qualification. Response
// shaping and the frozen-named grant helper stay in codequery for the
// same reason.
//
// Import discipline: this package may import the same dependency-neutral
// leaves codequery uses (codemodel, querycontract) but NEVER package
// codequery itself and NEVER root package query -- both would create an
// import cycle (codequery calls this leaf, and root aliases codequery).
package chain
