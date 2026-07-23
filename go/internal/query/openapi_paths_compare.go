// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsCompare documents the compare/* routes. Split from
// openAPIPathsStatusAndCompare to keep both files under the 500-line cap.
const openAPIPathsCompare = `
    "/api/v0/compare/environments": {
      "post": {
        "tags": ["compare"],
        "summary": "Compare environments",
        "description": "Compares a workload deployment across two environments and returns a prompt-ready story packet with shared resources, dedicated resources, evidence, limitations, and recommended next calls. Scoped tokens receive the same shape; a workload outside the caller's grant renders as not found.",
        "operationId": "compareEnvironments",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["workload_id", "left", "right"],
                "properties": {
                  "workload_id": {"type": "string"},
                  "left": {"type": "string", "description": "Left environment name"},
                  "right": {"type": "string", "description": "Right environment name"},
                  "limit": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Environment comparison",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "workload": {"type": "object"},
                    "left": {"type": "object"},
                    "right": {"type": "object"},
                    "changed": {"type": "object"},
                    "confidence": {"type": "number"},
                    "reason": {"type": "string"},
                    "story": {"type": "string"},
                    "summary": {"type": "object"},
                    "shared": {"type": "object"},
                    "dedicated": {"type": "object"},
                    "evidence": {"type": "array", "items": {"type": "object"}},
                    "limitations": {"type": "array", "items": {"type": "object"}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "coverage": {"type": "object"},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer metadata with schema_version, evidence_handles, missing_evidence, limitations, truncated, coverage, partial_reasons, and recommended_next_calls."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    }
  },
`
