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
                      "description": "Per-kind facet counts in graphEntityKinds display order. Owner: query/graph_entity_inventory_counts.go decodeGraphEntityKindCounts.",
                      "items": {
                        "type": "object",
                        "description": "One entity kind row. Owner: query/graph handler entity kinds.",
                        "properties": {
                          "kind": {"type": "string", "description": "Stable console-facing facet id from graphEntityKinds (services, repositories, libraries, container_images, environments, cloud_resources, identity_iam, networking). Owner: query/graph_entity_inventory.go graphEntityKind.key."},
                          "label": {"type": "string", "description": "Single graph label the facet counts (for example Workload for services). Owner: query/graph_entity_inventory.go graphEntityKind.label."},
                          "count": {"type": "integer", "description": "Fixed label total for the facet, including historical records excluded from entity lists. Owner: query/graph_entity_inventory_counts.go decodeGraphEntityKindCounts."}
                        }
                      }
                    },
                    "total": {"type": "integer", "description": "Sum of the per-kind facet counts. Owner: query/graph_entity_inventory.go listEntities."},
                    "kind": {"type": "string", "description": "Present only when a kind filter was applied; echoes the requested facet key. Owner: query/graph_entity_inventory.go listEntities."},
                    "entities": {
                      "type": "array",
                      "description": "Bounded name-searchable entity page for the requested kind, ordered by name; empty when no kind filter is applied. Owner: query/graph_entity_inventory.go listEntities via graphEntityKindListCypher.",
                      "items": {
                        "type": "object",
                        "description": "One name-searchable entity. Owner: query/graph handler.",
                        "properties": {
                          "id": {"type": "string", "description": "Graph node id (coalesced to empty when absent). Owner: query/graph_entity_inventory.go listEntities."},
                          "name": {"type": "string", "description": "Human-readable name from the facet nameProp (principal_value for identity_iam). Owner: query/graph_entity_inventory.go listEntities."},
                          "kind": {"type": "string", "description": "Facet key the row was listed under. Owner: query/graph_entity_inventory.go listEntities."},
                          "account": {"type": "string", "description": "Account or scope column (repo_id for services, account_id for cloud_resources, principal_account_id for identity_iam); empty when the facet label carries no account-like field. Owner: query/graph_entity_inventory.go listEntities."}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Rows in the entities page. Owner: query/graph_entity_inventory.go listEntities."},
                    "limit": {"type": "integer", "description": "Echo of the effective page bound (default 50, max 200). Owner: query/graph_entity_inventory.go graphEntityInventoryLimit."},
                    "offset": {"type": "integer", "description": "Echo of the requested page offset. Owner: query/graph_entity_inventory.go graphEntityInventoryOffset."},
                    "truncated": {"type": "boolean", "description": "True when more entity rows remain past this page. Owner: query/graph_entity_inventory.go listEntities."}
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
