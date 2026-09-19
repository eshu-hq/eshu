// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Playbooks is the OpenAPI path fragment documenting the
// `/api/v0/query-playbooks`, `/api/v0/query-playbooks/resolve` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Playbooks = `
    "/api/v0/query-playbooks": {
      "get": {
        "tags": ["query"],
        "summary": "List query playbooks",
        "description": "Returns the deterministic query playbook catalog. This is workflow-plan truth from static catalog data, not a live graph query. The default (compact) response returns id/name/version/prompt_family/description per playbook with deterministic limit/offset paging (#6795); pass view=full for the complete shape (required inputs, ordered steps, expected truth, evidence, and failure modes).",
        "operationId": "listQueryPlaybooks",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 20}, "description": "Maximum number of playbooks to return."},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Number of playbooks to skip for paging."},
          {"name": "view", "in": "query", "required": false, "schema": {"type": "string", "enum": ["compact", "full"], "default": "compact"}, "description": "compact (default) returns id/name/version/prompt_family/description; full returns the complete playbook shape."}
        ],
        "responses": {
          "200": {
            "description": "Query playbook catalog page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "playbooks": {"type": "array", "items": {"type": "object"}},
                    "versions": {"type": "array", "items": {"type": "object"}},
                    "count": {"type": "integer"},
                    "total": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": "integer", "nullable": true, "description": "offset to fetch the next page, or null when truncated is false."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"}
        }
      }
    },
    "/api/v0/query-playbooks/resolve": {
      "post": {
        "tags": ["query"],
        "summary": "Resolve a query playbook",
        "description": "Resolves one catalog playbook and declared inputs into an ordered, bounded call sequence. It does not execute the calls.",
        "operationId": "resolveQueryPlaybook",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["playbook_id"],
                "properties": {
                  "playbook_id": {"type": "string"},
                  "inputs": {"type": "object", "additionalProperties": {"type": "string"}}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Resolved bounded call sequence",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "resolved": {"type": "object"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"}
        }
      }
    },
`
