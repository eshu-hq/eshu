// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsReplatformingOwnership = `
    "/api/v0/replatforming/ownership-packets": {
      "post": {
        "tags": ["aws"],
        "summary": "Compose unmanaged-resource ownership packets",
        "description": "For each active reducer-materialized AWS runtime drift finding, composes a bounded ownership packet of owner, repository, module, service, and environment candidates with explicit ambiguity reasons, confidence, freshness, and the read-only safety gate. Candidates are derived from reducer-owned finding fields only; a single candidate is derived, never exact, and conflicting candidates are surfaced with ambiguity reasons rather than collapsed to a single guessed owner. Raw tags remain provenance-only and never become owner candidates. Requires the local-authoritative profile or higher; lower profiles return 501 unsupported_capability.",
        "operationId": "composeReplatformingOwnershipPackets",
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
                  "limit": {"type": "integer", "description": "Maximum findings to compose into the bounded page (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging the bounded page.", "default": 0}
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
            "description": "Bounded unmanaged-resource ownership packets",
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
                    "ownership_packets": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingOwnershipPacket"}},
                    "packets_count": {"type": "integer"},
                    "ambiguous_count": {"type": "integer"},
                    "unattributed_count": {"type": "integer"},
                    "rejected_count": {"type": "integer"},
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
