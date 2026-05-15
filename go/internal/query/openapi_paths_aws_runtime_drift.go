package query

const openAPIPathsAWSRuntimeDrift = `
    "/api/v0/aws/runtime-drift/findings": {
      "post": {
        "tags": ["aws"],
        "summary": "List AWS runtime drift findings",
        "description": "Lists active reducer-materialized AWS runtime drift findings for a bounded AWS scope or account. The response preserves evidence, missing/stale/ambiguous/unknown status, and rejected promotion state without exposing raw Cypher.",
        "operationId": "listAWSRuntimeDriftFindings",
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
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource.",
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
                          "evidence": {"type": "array", "items": {"type": "object"}}
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
