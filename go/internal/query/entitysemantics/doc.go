// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package entitysemantics renders an entity's promoted metadata into the
// prose summary and semantic profile that entity-shaped responses carry.
//
// AttachSemanticSummary writes a one-sentence, entity-type-specific summary
// onto a result map; the profile builders behind it interpret per-language
// metadata (Python promotion, TypeScript declaration merging, JavaScript
// framework shapes) into the fields that summary reads.
//
// It is a leaf rather than part of querycontract, and the distinction is the
// reason this package exists. querycontract holds the wire and port types
// several query families need without inheriting a runtime, and its
// AGENTS.md bars "handler orchestration, whole graph queries, SQL, or
// family-specific response models". Rendering entity prose is a
// family-specific response model: it decides how one family's answers read,
// not what any family's contract is. Landing it in querycontract would also
// have pushed that package further over the directory cap it already sits
// above under a tracked exemption (#6597).
//
// The precedent is codeshaping, which holds the method-free shaping helpers
// of the code query family for the same reason: shaping that belongs to a
// family, moved out of root so the family can be read and tested without the
// rest of the query surface.
//
// The package depends only on querycontract for row-value decoding. It never
// touches the graph, the content store, or the network, and root keeps
// lowercase forwarders so no existing caller changed when this moved (#6060).
package entitysemantics
