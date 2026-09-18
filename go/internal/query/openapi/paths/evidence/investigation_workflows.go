// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// InvestigationWorkflows is the OpenAPI path fragment documenting the
// `/api/v0/investigation-workflows`, `/api/v0/investigation-workflows/resolve`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const InvestigationWorkflows = `
    "/api/v0/investigation-workflows": {
      "get": {
        "tags": ["query"],
        "summary": "List guided investigation workflows",
        "description": "Returns the deterministic guided investigation workflow catalog. This is workflow-plan truth from static catalog data, not a live graph query. The default (compact) response returns id/name/version/domain/description per workflow with deterministic limit/offset paging (#6795); pass view=full for the complete shape (input shape, required/optional evidence, output packet, tool groups, starter prompts, and missing-evidence routing).",
        "operationId": "listInvestigationWorkflows",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 20}, "description": "Maximum number of workflows to return."},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Number of workflows to skip for paging."},
          {"name": "view", "in": "query", "required": false, "schema": {"type": "string", "enum": ["compact", "full"], "default": "compact"}, "description": "compact (default) returns id/name/version/domain/description; full returns the complete workflow shape."}
        ],
        "responses": {
          "200": {
            "description": "Guided investigation workflow catalog page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string"},
                    "workflows": {"type": "array", "items": {"type": "object"}},
                    "versions": {"type": "array", "items": {"type": "object"}},
                    "count": {"type": "integer"},
                    "total": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"}
        }
      }
    },
    "/api/v0/investigation-workflows/resolve": {
      "post": {
        "tags": ["query"],
        "summary": "Resolve a guided investigation workflow",
        "description": "Resolves one catalog workflow, declared inputs, and observed missing-evidence state into bounded recommended next calls. It does not execute the calls.",
        "operationId": "resolveInvestigationWorkflow",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["workflow_id"],
                "properties": {
                  "workflow_id": {"type": "string"},
                  "inputs": {"type": "object", "additionalProperties": {"type": "string"}},
                  "missing_evidence": {"type": "array", "items": {"type": "string"}}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Resolved recommended next calls",
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
