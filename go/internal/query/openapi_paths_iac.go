package query

const openAPIPathsIaC = `
    "/api/v0/iac/dead": {
      "post": {
        "tags": ["iac"],
        "summary": "Find dead IaC candidates",
        "description": "Finds bounded, content-derived dead-IaC candidates for explicit repository scopes. Dynamic references are returned as ambiguous until reducer-materialized usage rows make the result exact.",
        "operationId": "findDeadIaC",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string", "description": "Single repository ID to analyze."},
                  "repo_ids": {
                    "type": "array",
                    "description": "Explicit bounded repository scope to analyze.",
                    "items": {"type": "string"}
                  },
                  "families": {
                    "type": "array",
                    "description": "Optional IaC families to include: terraform, helm, kustomize, ansible, compose.",
                    "items": {"type": "string"}
                  },
                  "include_ambiguous": {"type": "boolean", "description": "Include dynamic-reference candidates.", "default": false},
                  "limit": {"type": "integer", "description": "Maximum findings to return (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging findings.", "default": 0}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Dead-IaC candidate findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_ids": {"type": "array", "items": {"type": "string"}},
                    "findings_count": {"type": "integer"},
                    "total_findings_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"]},
                    "truth_basis": {"type": "string"},
                    "analysis_status": {"type": "string"},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "family": {"type": "string"},
                          "repo_id": {"type": "string"},
                          "artifact": {"type": "string"},
                          "reachability": {"type": "string", "enum": ["unused", "ambiguous"]},
                          "finding": {"type": "string"},
                          "confidence": {"type": "number"},
                          "evidence": {"type": "array", "items": {"type": "string"}},
                          "limitations": {"type": "array", "items": {"type": "string"}}
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
    "/api/v0/iac/unmanaged-resources": {
      "post": {
        "tags": ["iac"],
        "summary": "Find unmanaged cloud resources",
        "description": "Finds AWS cloud resources whose active reducer drift facts show no Terraform config owner or only Terraform state ownership. Requests must be bounded by scope_id or account_id.",
        "operationId": "findUnmanagedResources",
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
            "description": "Unmanaged cloud resource findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string"},
                    "account_id": {"type": "string"},
                    "region": {"type": "string"},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}},
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
                    "findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "provider": {"type": "string"},
                          "account_id": {"type": "string"},
                          "region": {"type": "string"},
                          "resource_type": {"type": "string"},
                          "resource_id": {"type": "string"},
                          "arn": {"type": "string"},
                          "tags": {"type": "object", "additionalProperties": {"type": "string"}},
                          "finding_kind": {"type": "string"},
                          "management_status": {
                            "type": "string",
                            "enum": [
                              "managed_by_terraform",
                              "terraform_state_only",
                              "terraform_config_only",
                              "cloud_only",
                              "managed_by_other_iac",
                              "ambiguous_management",
                              "unknown_management",
                              "stale_iac_candidate"
                            ]
                          },
                          "confidence": {"type": "number"},
                          "scope_id": {"type": "string"},
                          "generation_id": {"type": "string"},
                          "source_system": {"type": "string"},
                          "candidate_id": {"type": "string"},
                          "matched_terraform_state_address": {"type": "string"},
                          "matched_terraform_config_file": {"type": "string"},
                          "matched_terraform_module_path": {"type": "string"},
                          "matched_other_iac_source": {"type": "string"},
                          "service_candidates": {"type": "array", "items": {"type": "string"}},
                          "environment_candidates": {"type": "array", "items": {"type": "string"}},
                          "dependency_paths": {"type": "array", "items": {"type": "string"}},
                          "recommended_action": {"type": "string"},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}},
                          "warning_flags": {"type": "array", "items": {"type": "string"}},
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
