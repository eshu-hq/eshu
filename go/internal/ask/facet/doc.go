// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package facet deterministically detects source-tool and language scope intent
// in an Ask Eshu question (#4006, epic #3997).
//
// Ask Eshu is an LLM-driven agentic loop over read-only MCP tools; it does not
// have a query catalog. facet does NOT execute any filter — the actual scoping
// happens server-side inside the MCP tool handlers (list_relationship_edges'
// source_tool filter, search_semantic_context's languages filter). facet's only
// job is to read the question text and report, deterministically and honestly,
// which canonical source_tool / language the user appears to be asking about, so
// the engine can steer the agent (via the system prompt) and the response can
// state the detected scope.
//
// Honesty contract:
//   - source_tool is reported only when the token is a member of the canonical
//     vocabulary (go/internal/sourcetool); a tool-like word that is not canonical
//     is reported as an UnknownToolMention so the answer can say "not a recognized
//     tool" instead of fabricating one.
//   - Collision-prone tokens that are also common English words (go, salt, chef,
//     cargo, pip, npm, maven) are reported only when a disambiguating qualifier
//     is present, to avoid false positives ("pinch of salt" is not Salt).
//   - When unsure, fields are left empty (no facet) rather than guessed.
//
// The result is detected INTENT, not a confirmed applied filter; callers must
// frame it that way (the query trace records the filters the agent actually
// applied).
package facet
