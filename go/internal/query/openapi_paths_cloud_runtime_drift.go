// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsCloudRuntimeDrift documents the provider-neutral multi-cloud
// runtime drift readback route (issues #1997, #1998). It surfaces reducer-owned
// reducer_multi_cloud_runtime_drift_finding rows (one per canonical
// cloud_resource_uid) with provider, normalized identity, finding_kind,
// management_status, provider-neutral source_state, and refusal-safety posture.
// The route is read-only, bounded, paginated, and truth-labeled; it never
// returns raw provider locators or raw evidence atoms, and refuses unsafe
// findings rather than omitting them. The one narrow exception is
// drifted_attributes (#5453): for an image_version_drift finding, it carries
// the bounded declared/observed value pairs (e.g. ami, image_uri, version)
// the finding is ABOUT -- a purpose-built projection of two evidence atoms
// per attribute, never the full raw evidence-atom list.
const openAPIPathsCloudRuntimeDrift = `
    "/api/v0/cloud/runtime-drift/findings": {
      "post": {
        "tags": ["cloud"],
        "summary": "List provider-neutral multi-cloud runtime drift findings",
        "description": "Lists active reducer-materialized provider-neutral runtime drift findings for a bounded canonical scope across aws, gcp, and azure. Filterable by provider, canonical cloud_resource_uid, and finding_kind. Each finding carries its provider-neutral source_state and safety gate; unsafe findings are reported as rejected with a refused action rather than omitted. local_lightweight returns unsupported_capability. Scoped tokens must supply a scope_id (or account_id/project_id/subscription_id alias) that resolves to a granted repository or ingestion scope; a scoped caller with no grants or an out-of-grant scope_id receives an empty page.",
        "operationId": "listCloudRuntimeDriftFindings",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_id": {"type": "string", "description": "Canonical ingestion scope, for example cloud-scope:gcp:project-synthetic."},
                  "account_id": {"type": "string", "description": "Alias for scope_id (AWS account scope)."},
                  "project_id": {"type": "string", "description": "Alias for scope_id (GCP project scope)."},
                  "subscription_id": {"type": "string", "description": "Alias for scope_id (Azure subscription scope)."},
                  "provider": {"type": "string", "description": "Optional provider filter: aws, gcp, or azure.", "enum": ["aws", "gcp", "azure"]},
                  "cloud_resource_uid": {"type": "string", "description": "Optional exact canonical resource uid to inspect."},
                  "finding_kinds": {
                    "type": "array",
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource.",
                    "items": {"type": "string"}
                  },
                  "limit": {"type": "integer", "description": "Maximum findings to return (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging findings.", "default": 0}
                },
                "anyOf": [
                  {"required": ["scope_id"]},
                  {"required": ["account_id"]},
                  {"required": ["project_id"]},
                  {"required": ["subscription_id"]}
                ]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Provider-neutral multi-cloud runtime drift findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string"},
                    "provider": {"type": "string"},
                    "cloud_resource_uid": {"type": "string"},
                    "story": {"type": "string"},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}},
                    "source_state_groups": {"type": "array", "items": {"type": "object"}},
                    "findings_count": {"type": "integer"},
                    "total_findings_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": "integer"},
                    "truth_basis": {"type": "string"},
                    "analysis_status": {"type": "string"},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "drift_findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "fact_id": {"type": "string"},
                          "provider": {"type": "string", "enum": ["aws", "gcp", "azure"]},
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "source_system": {"type": "string"},
                          "cloud_resource_uid": {"type": "string"},
                          "finding_kind": {"type": "string"},
                          "management_status": {"type": "string"},
                          "confidence": {"type": "number"},
                          "source_state": {"type": "string", "description": "Provider-neutral source-state taxonomy value derived from management_status and the safety gate."},
                          "matched_terraform_state_address": {"type": "string"},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}},
                          "recommended_action": {"type": "string"},
                          "drifted_attributes": {
                            "type": "array",
                            "description": "Bounded declared/observed value pairs for an image_version_drift finding (ami, image_uri, version, or the ECS container image comparison). Empty for orphaned/unmanaged/unknown/ambiguous findings.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "attribute": {"type": "string"},
                                "declared_value": {"type": "string"},
                                "observed_value": {"type": "string"}
                              }
                            }
                          },
                          "safety_gate": {"type": "object"}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
