// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// ReplatformingRollups is the OpenAPI path fragment documenting the
// `/api/v0/replatforming/rollups` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const ReplatformingRollups = `
    "/api/v0/replatforming/rollups": {
      "post": {
        "tags": ["aws"],
        "summary": "Roll up replatforming drift and readiness",
        "description": "Aggregates active reducer-materialized AWS runtime drift and IaC findings into bounded rollups by account, environment, and service over the provider-neutral source-state taxonomy plus an import-readiness view. Per-item source state is preserved; ambiguous or missing service/environment attribution is counted under explicit __ambiguous__ and __unattributed__ buckets and is never guessed.",
        "operationId": "rollupReplatformingReadiness",
        "x-scoped-token-support": true,
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
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id bound, emitted by the iac rollups handler from ManagementFilter.ScopeID."},
                    "account_id": {"type": "string", "description": "Echo of the caller's account_id bound, emitted by the iac rollups handler from ManagementFilter.AccountID."},
                    "region": {"type": "string", "description": "Echo of the caller's region bound, emitted by the iac rollups handler from ManagementFilter.Region."},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}, "description": "Echo of the caller's normalized finding-kind filter, emitted by the iac rollups handler from ManagementFilter.FindingKinds."},
                    "story": {"type": "string", "description": "One-line rollup summary rendered by iac.replatformingRollupsStory from the caller's filter and the rollup counts."},
                    "dimensions": {
                      "type": "object",
                      "properties": {
                        "account": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingRollupBucket"}, "description": "Rollup buckets keyed by account ID, emitted by the iac rollups handler from the rollup dimensions (component shape)."},
                        "environment": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingRollupBucket"}, "description": "Rollup buckets keyed by environment name, emitted by the iac rollups handler from the rollup dimensions (component shape)."},
                        "service": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingRollupBucket"}, "description": "Rollup buckets keyed by service name, emitted by the iac rollups handler from the rollup dimensions (component shape)."}
                      },
                      "description": "Each value is a list of rollup buckets keyed by account ID, environment name, or service name. The explicit __ambiguous__ and __unattributed__ keys hold contested and missing attribution."
                    },
                    "source_state_totals": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Count per source-state taxonomy value across the bounded page, emitted by the iac rollups handler from rollups.TotalStates. Keys are the taxonomy values (exact, derived, partial, ambiguous, stale, unavailable, unsupported, unknown, rejected)."},
                    "readiness_totals": {"$ref": "#/components/schemas/ReplatformingReadinessCounts", "description": "Bounded import-readiness view for the rollup, emitted by the iac rollups handler from rollups.Readiness (component shape)."},
                    "rollup_findings_count": {"type": "integer", "description": "Findings aggregated into this rollup, emitted by the iac rollups handler as len(findings)."},
                    "total_findings_count": {"type": "integer", "description": "Total active findings matching the filter, emitted by the iac rollups handler from the store count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the iac rollups handler from ManagementFilter.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the iac rollups handler from ManagementFilter.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by iac.ManagementTruncated."},
                    "next_offset": {"type": ["integer", "null"], "description": "Offset to fetch the next page (0 when not truncated), emitted by iac.ManagementNextOffset."},
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this rollup, emitted by the iac rollups handler (always materialized_reducer_rows)."},
                    "analysis_status": {"type": "string", "description": "Rollup status label emitted by the iac rollups handler (always replatforming_rollups)."},
                    "recommended_next_checks": {"type": "array", "items": {"type": "string"}, "description": "Fixed follow-up checks for this rollup, emitted by the iac rollups handler."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this rollup, emitted by the iac rollups handler."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
