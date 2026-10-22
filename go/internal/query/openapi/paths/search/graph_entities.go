// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

// GraphEntities is the OpenAPI path fragment documenting the
// `/api/v0/graph/entities` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const GraphEntities = `
    "/api/v0/graph/entities": {
      "get": {
        "tags": ["infrastructure"],
        "summary": "Browse graph entity inventory",
        "description": "Returns per-kind counts of stored graph nodes plus, when kind is set, a bounded, name-searchable, paginated slice of visible first-class entities for that kind. Counts are fixed label totals and can include historical records excluded from entity lists; q, limit, and offset affect only the list. One graph round trip returns all eight facet counts through scalar label-anchored subqueries; the optional entity list is anchored on one label, ordered by name, and limited, keeping the read within the interactive SLA. A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable; this route has no configuration 503, so every 503 it answers carries Retry-After. A graph read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.context_overview in its error envelope.",
        "operationId": "browseGraphEntityInventory",
        "parameters": [
          {"name": "kind", "in": "query", "required": false, "description": "Facet key to list. Omit to return only the per-kind counts.", "schema": {"type": "string", "enum": ["services", "repositories", "libraries", "container_images", "environments", "cloud_resources", "identity_iam", "networking"]}},
          {"name": "q", "in": "query", "required": false, "description": "Case-insensitive substring match on the entity name. Only applied when kind is set.", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Graph entity inventory",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "kinds": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "kind": {"type": "string"},
                          "label": {"type": "string"},
                          "count": {"type": "integer"}
                        }
                      }
                    },
                    "total": {"type": "integer"},
                    "kind": {"type": "string", "description": "Present only when a kind filter was applied."},
                    "entities": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "name": {"type": "string"},
                          "kind": {"type": "string"},
                          "account": {"type": "string"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },`
