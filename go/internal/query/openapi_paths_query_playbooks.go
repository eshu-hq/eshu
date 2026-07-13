// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsQueryPlaybooks = `
    "/api/v0/query-playbooks": {
      "get": {
        "tags": ["query"],
        "summary": "List query playbooks",
        "description": "Returns the deterministic query playbook catalog. This is workflow-plan truth from static catalog data, not a live graph query.",
        "operationId": "listQueryPlaybooks",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Query playbook catalog",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "playbooks": {"type": "array", "items": {"type": "object"}},
                    "versions": {"type": "array", "items": {"type": "object"}},
                    "count": {"type": "integer"}
                  }
                }
              }
            }
          }
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
