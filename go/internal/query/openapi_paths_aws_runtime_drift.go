// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsAWSRuntimeDrift = `
    "/api/v0/aws/runtime-drift/findings": {
      "post": {
        "tags": ["aws"],
        "summary": "List AWS runtime drift findings",
        "description": "Lists active reducer-materialized AWS runtime drift findings for a bounded AWS scope or account. The response preserves evidence, missing/stale/ambiguous/unknown status, and rejected promotion state without exposing raw Cypher. Scoped tokens must supply an exact scope_id that resolves to a granted repository or ingestion scope; an account_id-only request (which fans out across every region/service scope under that account) or an out-of-grant scope_id receives an empty page.",
        "operationId": "listAWSRuntimeDriftFindings",
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
                  "arn": {"type": "string", "description": "Optional exact AWS ARN to inspect."},
                  "finding_kinds": {
                    "type": "array",
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, ambiguous_cloud_resource, or image_version_drift.",
                    "items": {"type": "string"}
                  },
                  "limit": {"type": "integer", "description": "Maximum findings to return (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging findings.", "default": 0}
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
            "description": "AWS runtime drift findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string"},
                    "account_id": {"type": "string"},
                    "region": {"type": "string"},
                    "arn": {"type": "string"},
                    "story": {"type": "string"},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}},
                    "outcome_groups": {"type": "array", "items": {"type": "object"}},
                    "findings_count": {"type": "integer"},
                    "total_findings_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"]},
                    "truth_basis": {"type": "string"},
                    "analysis_status": {"type": "string"},
                    "graph_projection_note": {"type": "string"},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "drift_findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "arn": {"type": "string"},
                          "provider": {"type": "string"},
                          "account_id": {"type": "string"},
                          "region": {"type": "string"},
                          "finding_kind": {"type": "string"},
                          "management_status": {"type": "string"},
                          "outcome": {"type": "string", "enum": ["exact", "derived", "ambiguous", "stale", "unknown"]},
                          "promotion_outcome": {"type": "string", "enum": ["not_promoted", "rejected"]},
                          "promotion_reason": {"type": "string"},
                          "confidence": {"type": "number"},
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "source_system": {"type": "string"},
                          "service_candidates": {"type": "array", "items": {"type": "string"}},
                          "environment_candidates": {"type": "array", "items": {"type": "string"}},
                          "dependency_paths": {"type": "array", "items": {"type": "string"}},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}},
                          "warning_flags": {"type": "array", "items": {"type": "string"}},
                          "recommended_action": {"type": "string"},
                          "safety_gate": {"type": "object"},
                          "evidence": {"type": "array", "items": {"type": "object"}},
                          "drifted_attributes": {
                            "type": "array",
                            "description": "Bounded declared/observed value pairs for an image_version_drift finding (ami, image_uri, version, or the ECS container image comparison). Empty for every other finding kind.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "attribute": {"type": "string"},
                                "declared_value": {"type": "string"},
                                "observed_value": {"type": "string"}
                              }
                            }
                          }
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
