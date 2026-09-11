// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package owners projects Repository-[:DECLARES_CODEOWNER]->CodeownerTeam
// edges from directly-emitted codeowners.ownership facts (issue #5419
// Phase 3, moved out of the reducer root under issue #6061). [Handler.Handle]
// mirrors the documentation-edge family: codeowners.ownership is a
// direct-emitted fact (not a parser entity), so the reducer consumes it and
// rides the shared-projection intent-queue path rather than the
// canonical-projector entity path.
//
// [ExtractOwnershipEdgeRowsWithQuarantine] decodes every codeowners.ownership
// envelope and builds one DECLARES_CODEOWNER edge row per (pattern, owner)
// pair, deduplicating repeated (repo, path, pattern, owner) keys onto the
// highest order_index seen (GitHub's CODEOWNERS resolution is
// last-match-wins). [deltaScope] mirrors the inheritance family's delta
// scope, and a delta whose changed paths touch one of the three recognized
// CODEOWNERS candidate locations forces a whole-repository retract instead
// of the ordinary path-scoped one, because CODEOWNERS winner-resolution is
// whole-repo (issue #5419 P1).
//
// The reducer root imports this package as owners. It keeps the exported
// CodeownersOwnershipEdgeMaterializationHandler/
// ExtractCodeownersOwnershipEdgeRowsWithQuarantine spellings, and the
// unexported codeownersMaterializationFactKinds/
// loadCodeownersOwnershipMaterializationFacts call sites, through the
// codeowners stanza of the reducer root's compat surface.
//
// Dependency rule: from the reducer tree this package imports only the
// shared tier (contract, factdecode, factload, payloadcore, schemadecode,
// sharedintent); outside it, facts, the telemetry package, and the standard
// library. It never imports the reducer root.
package owners
