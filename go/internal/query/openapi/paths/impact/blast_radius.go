// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// blastRadius is the OpenAPI path fragment documenting the
// `/api/v0/impact/blast-radius` route. It is folded into Routes by a
// source-level `+` in routes.go rather than joined again in
// openapi/spec.go; do not add it there or the route doubles in the
// published document. See README.md.
const blastRadius = `
    "/api/v0/impact/blast-radius": {
      "post": {
        "tags": ["impact"],
        "summary": "Find blast radius",
        "description": "Analyzes the blast radius for a target entity. Scoped tokens receive the same shape; affected repositories outside the caller's grant are withheld.",
        "operationId": "findBlastRadius",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["target", "target_type"],
                "properties": {
                  "target": {"type": "string", "description": "Target entity name"},
                  "target_type": {
                    "type": "string",
                    "enum": ["repository", "terraform_module", "crossplane_xrd", "sql_table"],
                    "description": "Type of target entity"
                  },
                  "limit": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Blast radius analysis",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "target": {"type": "string", "description": "Target entity name echoed from the request by findBlastRadius (impact/blast_radius.go)."},
                    "target_type": {"type": "string", "description": "Target entity type echoed from the request by findBlastRadius."},
                    "affected": {"type": "array", "description": "Affected repository rows from findBlastRadius: repo is always present; tier and risk come from the tier enrichment, hops from the traversal distance, and repo_id and claim from the graph row when present.", "items": {"type": "object", "description": "Affected repository rows from findBlastRadius.", "required": ["repo"], "properties": {"repo": {"type": "string", "description": "Display name of the affected repository; always emitted by findBlastRadius."}, "tier": {"type": "string", "description": "Deployment tier of the affected repo, present when the tier enrichment resolved one."}, "risk": {"type": "string", "description": "Tier risk level, present when the tier enrichment resolved one."}, "hops": {"type": "integer", "description": "Shortest traversal distance to the target, present when positive."}, "repo_id": {"type": "string", "description": "Canonical id of the affected repository, present when the graph row carries one."}, "claim": {"type": "string", "description": "Representative claim name, present only for crossplane_xrd rows."}}}},
                    "affected_count": {"type": "integer", "description": "Number of affected repository rows emitted by findBlastRadius."},
                    "limit": {"type": "integer", "description": "Applied row bound echoed by findBlastRadius."},
                    "truncated": {"type": "boolean", "description": "True when the affected set hit the limit and was trimmed by findBlastRadius."},
                    "complete": {
                      "type": "boolean",
                      "description": "False when target_type is sql_table or crossplane_xrd and any edge type the surface conceptually covers (see coverage) has no graph writer, so affected_count may undercount. True for crossplane_xrd today (CONTAINS and SATISFIED_BY both have graph writers as of #5347). Always true for target_type values with no registered coverage gap (repository, terraform_module). Omitted on the empty-grant early return, which carries no affected rows."
                    },
                    "coverage": {
                      "type": "array",
                      "description": "Materialization status of every graph edge type the target_type's blast-radius surface conceptually covers. Empty for target_type values with no registered coverage gap.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "edge_type": {"type": "string", "description": "Graph relationship type, e.g. INDEXES"},
                          "materialized": {"type": "boolean", "description": "Whether a graph writer currently emits this edge type"},
                          "reason": {"type": "string", "description": "Why the edge type is or is not materialized"}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
