// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entityresolutiontools

import (
	"net/url"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for an entity-resolution tool
// without executing it. It reports handled only for the three tools this
// package owns. Family membership is an explicit name switch, never a prefix
// match: search_entity_content shares the entity spelling but stays in the
// root switch because its whole body comes from contentSearchBody, the
// builder it shares with search_file_content, and that pair's shared wire
// shape must keep one owner.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "resolve_entity":
		return routecontract.Request{Method: "POST", Path: "/api/v0/entities/resolve", Body: resolveEntityBody(args)}, true
	case "get_entity_context":
		query := map[string]string{}
		if environment := args.String("environment"); environment != "" {
			query["environment"] = environment
		}
		return routecontract.Request{
			Method: "GET",
			Path:   "/api/v0/entities/" + url.PathEscape(args.String("entity_id")) + "/context",
			Query:  query,
		}, true
	case "get_entity_content":
		return routecontract.Request{Method: "POST", Path: "/api/v0/content/entities/read", Body: map[string]any{
			"entity_id": args.String("entity_id"),
		}}, true
	default:
		return routecontract.Request{}, false
	}
}

// resolveEntityBody preserves the exact wire shape the root switch arm sent
// through the former root helper of the same name. Every key except limit is
// conditional: name maps from the advertised query argument when the
// deprecated name alias is blank and is omitted when both are blank (the
// handler then rejects with HTTP 400 "name is required"); type prefers the
// single type argument and falls back to the first element of the deprecated
// types array — a non-empty array whose first element is not a string still
// sets type, to an empty string, exactly as the root helper did; repo_id
// travels only when non-empty. limit always travels and defaults to 10, the
// same value the handler substitutes for a nonpositive limit before capping
// anything above 100 at 100, so no limit value can 400.
func resolveEntityBody(args routecontract.Arguments) map[string]any {
	body := map[string]any{"limit": args.IntOr("limit", 10)}

	if name := args.String("name"); name != "" {
		body["name"] = name
	} else if query := args.String("query"); query != "" {
		body["name"] = query
	}
	if kind := args.String("type"); kind != "" {
		body["type"] = kind
	} else if kinds := args.StringSlice("types"); len(kinds) > 0 {
		first, _ := kinds[0].(string)
		body["type"] = first
	}
	if repoID := args.String("repo_id"); repoID != "" {
		body["repo_id"] = repoID
	}

	return body
}

// The three requests preserve the exact shapes the root switch arms sent.
//
// get_entity_context is the family's one GET: the canonical entity id is
// path-escaped into /api/v0/entities/{entity_id}/context, and the
// environment argument travels as a query parameter only when the caller
// supplied a non-empty string — an always-non-nil, possibly empty query map,
// pinned because the root arm built the same shape. The handler
// (query/entity.go getEntityContext) reads only the entity_id path
// parameter and decodes no query parameter at all, so environment is an
// advertised-versus-decoded asymmetry inherited from the schema, not a
// dropped field.
//
// get_entity_content always sends entity_id, as an empty string when the
// argument is absent or wrong-typed; the handler (query/content_handler.go
// readEntity) decodes only entity_id and rejects the empty string with HTTP
// 400 "entity_id is required".
