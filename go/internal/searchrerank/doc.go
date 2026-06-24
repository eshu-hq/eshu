// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchrerank reorders already-retrieved, in-scope search results
// around code-to-cloud graph anchors (service story, deployment unit,
// environment, incident, package, owner) without issuing any graph read or
// write.
//
// The reranker is a pure, deterministic function over the baseline retrieval
// results it is given. It never adds, removes, or relabels a result: it only
// reorders the existing in-scope set and records a ranking basis that preserves
// each result's baseline lexical/vector score. Graph proximity is derived
// entirely from the graph handles already carried on each curated document, so
// the package keeps the same boundaries as searchhybrid: no Cypher, no hosted
// call, and no promotion of a search score to canonical graph truth.
//
// Reranking is opt-in and fails closed to the baseline order in three cases: it
// is disabled, the supplied graph context is marked stale, or no graph signal
// fires for any result. The resolved State records which path answered so
// operators and clients can tell measured reranking from a baseline fallback.
package searchrerank
