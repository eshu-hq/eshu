// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rank implements the deterministic lexical router.Ranker behind the
// MCP router surface's find_tool (epic #8752, issue #8755).
//
// Lexical scores each tool with BM25 over its name, curated catalog intents,
// and description, weighting intents and name above the description. Wording
// repeated across many descriptions, such as the scoped-token refusal sentence,
// is removed before scoring. Input-schema property names let supplied anchors
// prefer the tools that accept them. Equal scores order by Canonical, cheaper
// Cost, then name, so the same query on the same build always returns the same
// order.
//
// The corpus index is built once by New. Rank filters with the caller's
// allowed func before it scores, so a hidden tool never appears and never
// shifts a visible tool's score. The package holds no runtime state beyond the
// immutable index, reads no configuration, and emits no telemetry; the
// find_tool dispatch lane owns outcome metrics.
//
// The package imports the router contract but never the mcp root, which
// injects the ranker.
package rank
