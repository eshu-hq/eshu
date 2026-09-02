// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package entityresolutiontools defines pure route selection for the MCP
// entity-resolution family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order (resolve_entity and
// get_entity_context stay in the root context group in tools_context.go,
// and get_entity_content in tools_content.go), global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded
// reads behind POST /api/v0/entities/resolve,
// GET /api/v0/entities/{entity_id}/context, and
// POST /api/v0/content/entities/read. This package runs no query and must
// keep every tool name, request method, path, body key, and query key
// stable.
//
// resolve_entity's body is almost entirely conditional: name maps from the
// advertised query argument when the deprecated name alias is blank and is
// omitted when both are blank (the handler rejects a missing name with HTTP
// 400); type prefers the single type argument and falls back to the first
// element of the deprecated types array, including to an explicit empty
// string when that first element is not a string; repo_id travels only when
// non-empty. limit always travels and defaults to 10, the same value the
// handler substitutes for a nonpositive limit before capping anything above
// 100 at 100, so no limit value can 400. Everything else is bounded at the
// handler: a global call without repo_id requires a supported type or a
// canonical content-entity handle, graph-only types require repo_id, and an
// unknown non-blank type rejects with HTTP 400.
//
// get_entity_context is the family's one GET: the entity id is path-escaped
// into the URL and environment travels as a query parameter only when
// non-empty, in an always-non-nil query map — the exact shape the root arm
// built. The handler decodes no query parameter at all, so environment is an
// inherited advertised-versus-decoded asymmetry, not a dropped field.
// get_entity_content always sends entity_id, as an empty string when absent,
// which the handler rejects with HTTP 400.
//
// search_entity_content is deliberately not part of this family: its whole
// body comes from contentSearchBody, the root builder it shares with
// search_file_content, so the pair's shared wire shape keeps one owner in
// the root switch until the content family moves together.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "17" becomes the default rather than
// an error. Wrong-typed strings read as empty.
package entityresolutiontools
