// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsGraphEntities = `
    "/api/v0/graph/entities": {
      "get": {
        "tags": ["infrastructure"],
        "summary": "Browse graph entity inventory",
        "description": "Returns the browsable graph entity inventory that backs the console Nodes page: per-kind facet counts always, plus a bounded, name-searchable, paginated slice of one kind's first-class entities when kind is set. One graph round trip returns all eight facet counts through scalar label-anchored subqueries; the optional entity list is anchored on one label, ordered by name, and limited, keeping the read within the interactive SLA.",
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
