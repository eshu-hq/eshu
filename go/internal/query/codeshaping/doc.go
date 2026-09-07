// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeshaping implements the method-free shaping helpers of the code
// query family: the dead-code candidate scan schedule
// (DeadCodeCandidateSchedule with its scan bounds) and the relationship-story
// response shapers (bounded-centrality ranking and token-budget trimming).
//
// These are pure in-process shapers over already-fetched rows: the schedule
// round-robins candidate labels under one shared row ceiling without
// multiplying downstream hydration and reachability work, the centrality
// ranker reorders the bounded result set by neighbor degree, and the budget
// applier trims rows in place to a caller token budget. None of them touches
// the graph, the content store, or the network.
//
// They moved out of root package query (#6060 lane A L3) so the family can
// be read, tested, and changed without pulling in the rest of the query
// surface. The package may import only dependency-neutral leaves --
// querycontract (row-value decoders) and codemodel (the already-moved
// RelationshipStoryRequest) -- never root package query itself, which would
// create an import cycle: root's family_code_shim_shaping.go imports this
// package for the compatibility aliases the staying dead-code readers, the
// lane-B content reader, and the staying tests still use.
//
// Three contracts callers must hold. Every scan stays bounded: the display
// limit fans out through DeadCodeCandidateQueryLimit into
// DeadCodeCandidateScanLimit, and pages retire sparse labels after their
// first short page. Ranking stays in-process over the already-bounded rows
// (n <= limit+1 per direction/type), so it adds no graph query. The budget
// applier keeps incoming row order and only trims, so callers that want the
// most useful rows to survive a small budget must order rows by relevance
// first.
//
// See README.md for the full boundary and AGENTS.md for the per-symbol
// export list.
package codeshaping
