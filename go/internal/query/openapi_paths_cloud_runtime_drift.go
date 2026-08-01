// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsCloudRuntimeDrift documents the runtime drift readback route
// across all three providers (issues #1997, #1998, #5759 follow-up). It
// aggregates reducer_multi_cloud_runtime_drift_finding rows (gcp, azure; one
// per canonical cloud_resource_uid) with reducer_aws_cloud_runtime_drift_finding
// rows (aws) in one query, so provider=aws and an unfiltered query genuinely
// return AWS findings instead of the empty page this route returned for aws
// before the aggregation existed. Findings carry provider, normalized
// identity, finding_kind, management_status, provider-neutral source_state,
// and refusal-safety posture. The route is read-only, bounded, paginated, and
// truth-labeled; it never returns raw provider locators (including the AWS
// ARN) or raw evidence atoms, and refuses unsafe findings rather than
// omitting them. drifted_attributes (#5453) is a narrow exception: for an
// image_version_drift finding, it carries the bounded declared/observed value
// pairs (e.g. ami, image_uri, version) the finding is ABOUT -- a
// purpose-built projection of two evidence atoms per attribute, never the
// full raw evidence-atom list. An aws-origin finding's management_status,
// missing_evidence, and warning_flags (folded into safety_gate) are derived
// through the SAME classification list_aws_runtime_drift_findings uses
// (awsCloudRuntimeDriftDerivedStatus, #5759 follow-up P1-1), so the identical
// underlying reducer row never produces two different safety verdicts
// depending on which route reads it.
const openAPIPathsCloudRuntimeDrift = `
    "/api/v0/cloud/runtime-drift/findings": {
      "post": {
        "tags": ["cloud"],
        "summary": "List runtime drift findings across aws, gcp, and azure",
        "description": "Lists active reducer-materialized runtime drift findings for a bounded canonical scope, aggregated across all three providers: reducer_multi_cloud_runtime_drift_finding (gcp, azure) and reducer_aws_cloud_runtime_drift_finding (aws) in one query. Filterable by provider, canonical cloud_resource_uid, and finding_kind; cloud_resource_uid filtering matches only gcp/azure findings (an AWS finding's canonical identity is resolved for display but is not a stored, filterable column on its fact kind). Each finding carries its provider-neutral source_state and safety gate; unsafe findings are reported as rejected with a refused action rather than omitted. local_lightweight returns unsupported_capability. Scoped tokens must supply a scope_id (or account_id/project_id/subscription_id alias) that resolves to a granted repository or ingestion scope; a scoped caller with no grants or an out-of-grant scope_id receives an empty page.",
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
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, ambiguous_cloud_resource, image_version_drift, or value_comparison_inconclusive.",
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
                            "description": "Bounded declared/observed value pairs for an image_version_drift finding (ami, image_uri, version, or the ECS container image comparison). Empty for every other kind, including value_comparison_inconclusive, which reports that no comparison could be made at all and names the unreadable attributes in missing_evidence instead.",
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
