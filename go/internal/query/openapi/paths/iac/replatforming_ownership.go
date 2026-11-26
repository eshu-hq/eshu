// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// ReplatformingOwnership is the OpenAPI path fragment documenting the
// `/api/v0/replatforming/ownership-packets` route. openapi.Spec concatenates
// it into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const ReplatformingOwnership = `
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
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id bound, emitted by the iac ownership-packets handler from ManagementFilter.ScopeID."},
                    "account_id": {"type": "string", "description": "Echo of the caller's account_id bound, emitted by the iac ownership-packets handler from ManagementFilter.AccountID."},
                    "region": {"type": "string", "description": "Echo of the caller's region bound, emitted by the iac ownership-packets handler from ManagementFilter.Region."},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}, "description": "Echo of the caller's normalized finding-kind filter, emitted by the iac ownership-packets handler from ManagementFilter.FindingKinds."},
                    "story": {"type": "string", "description": "One-line composition summary rendered by iac.replatformingOwnershipStory from the caller's filter and the packet counts."},
                    "ownership_packets": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingOwnershipPacket"}, "description": "Bounded ownership packets, emitted by the iac ownership-packets handler from buildReplatformingOwnershipSummary as ReplatformingOwnershipPacket views (component shape)."},
                    "packets_count": {"type": "integer", "description": "Packets composed in this page, emitted by the iac ownership-packets handler as len(summary.Packets)."},
                    "ambiguous_count": {"type": "integer", "description": "Packets with conflicting candidates, counted by the iac ownership-packets handler from summary.AmbiguousCount."},
                    "unattributed_count": {"type": "integer", "description": "Packets with no attribution, counted by the iac ownership-packets handler from summary.UnattributedCount."},
                    "rejected_count": {"type": "integer", "description": "Packets refused by the safety gate, counted by the iac ownership-packets handler from summary.RejectedCount."},
                    "total_findings_count": {"type": "integer", "description": "Total active findings inspected for this page, emitted by the iac ownership-packets handler from the store count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the iac ownership-packets handler from ManagementFilter.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the iac ownership-packets handler from ManagementFilter.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by iac.ManagementTruncated."},
                    "next_offset": {"type": ["integer", "null"], "description": "Offset to fetch the next page (0 when not truncated), emitted by iac.ManagementNextOffset."},
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this composition, emitted by the iac ownership-packets handler (always materialized_reducer_rows)."},
                    "analysis_status": {"type": "string", "description": "Composition status label emitted by the iac ownership-packets handler (always replatforming_ownership_packets)."},
                    "recommended_next_checks": {"type": "array", "items": {"type": "string"}, "description": "Fixed follow-up checks for ambiguous and refused packets, emitted by the iac ownership-packets handler."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this composition, emitted by the iac ownership-packets handler."}
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
