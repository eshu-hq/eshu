// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// DocumentationFindingAggregate is the OpenAPI path fragment documenting the
// `/api/v0/documentation/findings/count`,
// `/api/v0/documentation/findings/inventory` routes. openapi.Spec concatenates
// it into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const DocumentationFindingAggregate = `
    "/api/v0/documentation/findings/count": {
      "get": {
        "summary": "Count durable documentation findings without paging the list endpoint",
        "description": "Counts durable documentation findings by status, truth level, and freshness state. A 503 with Retry-After means a transient condition that is safe to retry: the aggregate read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the documentation finding aggregate store is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 names capability documentation_findings.aggregate in its error envelope.",
        "operationId": "countDocumentationFindings",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "finding_type", "in": "query", "schema": {"type": "string"}},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "status", "in": "query", "schema": {"type": "string"}},
          {"name": "truth_level", "in": "query", "schema": {"type": "string"}},
          {"name": "freshness_state", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Documentation finding totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_findings": {"type": "integer"},
                    "by_status": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_truth_level": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_freshness_state": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Not implemented: the runtime profile does not support documentation_findings.aggregate (local_lightweight does not). The route answers error code unsupported_capability: the plain body carries error_code, message, capability, and correlation_id, and the envelope carries the same fields plus the current and required profiles.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/documentation/findings/inventory": {
      "get": {
        "summary": "Group durable documentation findings by one dimension without paging the list endpoint",
        "description": "Groups durable documentation findings into count buckets by one dimension. A 503 with Retry-After means a transient condition that is safe to retry: the aggregate read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the documentation finding aggregate store is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 names capability documentation_findings.aggregate in its error envelope.",
        "operationId": "getDocumentationFindingInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["status", "truth_level", "freshness_state", "finding_type", "source_id"], "default": "status"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "finding_type", "in": "query", "schema": {"type": "string"}},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "status", "in": "query", "schema": {"type": "string"}},
          {"name": "truth_level", "in": "query", "schema": {"type": "string"}},
          {"name": "freshness_state", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 10000, "default": 0}}
        ],
        "responses": {
          "200": {
            "description": "Grouped count buckets ordered by count desc",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "buckets": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "dimension": {"type": "string"},
                          "value": {"type": "string"},
                          "count": {"type": "integer"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "group_by": {"type": "string"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Not implemented: the runtime profile does not support documentation_findings.aggregate (local_lightweight does not). The route answers error code unsupported_capability: the plain body carries error_code, message, capability, and correlation_id, and the envelope carries the same fields plus the current and required profiles.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
