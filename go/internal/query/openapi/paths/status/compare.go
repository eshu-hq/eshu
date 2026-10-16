// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Compare documents the compare/* routes. Split from
// Routes to keep both files under the 500-line cap.
const Compare = `
    "/api/v0/compare/environments": {
      "post": {
        "tags": ["compare"],
        "summary": "Compare environments",
        "description": "Compares a workload deployment across two environments and returns a prompt-ready story packet with shared resources, dedicated resources, evidence, limitations, and recommended next calls. Scoped tokens receive the same shape; a workload outside the caller's grant renders as not found. A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable, or the service evidence read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. The service evidence read on a stale or timed-out reader answers the retryable 503 (it answered 500 before #7674). A graph read that exceeds its deadline answers 504. Any other failed read answers 500 with a fixed message that names the read. No 5xx body carries backend error text. A reader-verdict 503 names capability platform_impact.environment_compare in its error envelope.",
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
