// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsReplatformingRollups = `
    "/api/v0/replatforming/rollups": {
      "post": {
        "tags": ["aws"],
        "summary": "Roll up replatforming drift and readiness",
        "description": "Aggregates active reducer-materialized AWS runtime drift and IaC findings into bounded rollups by account, environment, and service over the provider-neutral source-state taxonomy plus an import-readiness view. Per-item source state is preserved; ambiguous or missing service/environment attribution is counted under explicit __ambiguous__ and __unattributed__ buckets and is never guessed.",
        "operationId": "rollupReplatformingReadiness",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_id": {"type": "string", "description": "Exact AWS collector scope, for example aws:123456789012:us-east-1:lambda."},
                  "account_id": {"type": "string", "description": "AWS account ID used to bound the active finding read."},
                  "region": {"type": "string", "description": "Optional AWS region when account_id is supplied."},
                  "finding_kinds": {
                    "type": "array",
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource.",
                    "items": {"type": "string"}
                  },
                  "limit": {"type": "integer", "description": "Maximum findings to aggregate into the bounded rollup (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging the bounded rollup.", "default": 0}
                },
                "anyOf": [
                  {"required": ["scope_id"]},
                  {"required": ["account_id"]}
                ]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Replatforming drift and readiness rollups",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string"},
                    "account_id": {"type": "string"},
                    "region": {"type": "string"},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}},
                    "story": {"type": "string"},
                    "dimensions": {
                      "type": "object",
                      "properties": {
                        "account": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingRollupBucket"}},
                        "environment": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingRollupBucket"}},
                        "service": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingRollupBucket"}}
                      },
                      "description": "Each value is a list of rollup buckets keyed by account ID, environment name, or service name. The explicit __ambiguous__ and __unattributed__ keys hold contested and missing attribution."
                    },
                    "source_state_totals": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Account-wide count per source-state taxonomy value across the bounded page."},
                    "readiness_totals": {"$ref": "#/components/schemas/ReplatformingReadinessCounts"},
                    "rollup_findings_count": {"type": "integer"},
                    "total_findings_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"]},
                    "truth_basis": {"type": "string"},
                    "analysis_status": {"type": "string"},
                    "recommended_next_checks": {"type": "array", "items": {"type": "string"}},
                    "limitations": {"type": "array", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/ServiceUnavailable"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
