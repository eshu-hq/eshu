// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsReplatforming documents the service-scoped replatforming plan
// compose route. The route composes a provider-neutral ReplatformingPlan over
// reducer-owned IaC management and runtime-drift evidence and is read-only: it
// never runs Terraform, imports resources, or mutates cloud or repository state.
const openAPIPathsReplatforming = `
    "/api/v0/replatforming/plans": {
      "post": {
        "tags": ["iac"],
        "summary": "Compose a service-scoped replatforming plan",
        "description": "Composes one bounded, truth-labeled replatforming plan over active AWS IaC management and runtime-drift findings. Each migration packet item carries its management status, finding kind, provider-neutral source state, safety gate, source layers, owner candidates with ambiguity reasons, and a ready or refused Terraform import candidate. The route is read-only: it never runs Terraform, imports resources, or mutates cloud or repository state. Lightweight local profiles return unsupported_capability.",
        "operationId": "composeReplatformingPlan",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_kind": {"type": "string", "description": "Primary plan scope dimension: account, region, service, workload, repository, environment, or resource.", "enum": ["account", "region", "service", "workload", "repository", "environment", "resource"]},
                  "scope_id": {"type": "string", "description": "Exact AWS collector scope, for example aws:123456789012:us-east-1:lambda."},
                  "account_id": {"type": "string", "description": "AWS account ID used to bound the active finding read."},
                  "region": {"type": "string", "description": "Optional AWS region when account_id is supplied."},
                  "service_name": {"type": "string", "description": "Optional service name that narrows the plan scope."},
                  "workload_id": {"type": "string", "description": "Optional deployable workload identity that narrows the plan scope."},
                  "repo_id": {"type": "string", "description": "Optional source repository identity that narrows the plan scope."},
                  "environment": {"type": "string", "description": "Optional environment that narrows the plan scope."},
                  "arn": {"type": "string", "description": "Optional exact AWS ARN to inspect."},
                  "resource_id": {"type": "string", "description": "Optional alias for arn; for AWS this must be the full ARN, not a provider-local ID such as an S3 bucket name or Lambda function name."},
                  "finding_kinds": {
                    "type": "array",
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource.",
                    "items": {"type": "string"}
                  },
                  "limit": {"type": "integer", "description": "Maximum migration packet items to compose (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging migration packet items.", "default": 0}
                },
                "required": ["scope_kind"],
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
            "description": "Composed replatforming plan",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "story": {"type": "string"},
                    "scope_kind": {"type": "string"},
                    "account_id": {"type": "string"},
                    "scope_id": {"type": "string"},
                    "region": {"type": "string"},
                    "arn": {"type": "string"},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}},
                    "plan": {
                      "type": "object",
                      "properties": {
                        "contract_version": {"type": "string"},
                        "scope": {"type": "object"},
                        "items": {"type": "array", "items": {"type": "object"}},
                        "waves": {"type": "array", "description": "Ordered migration waves for staged migration; each wave lists item_ids and a rationale. wave-1-early-safe holds import-ready, low-blast-radius, non-gated items; wave-2-review holds non-gated items needing review; wave-3-blocked holds safety-gated, rejected, or ambiguously owned items and is always last.", "items": {"type": "object", "properties": {"id": {"type": "string"}, "order": {"type": "integer"}, "rationale": {"type": "string"}, "item_ids": {"type": "array", "items": {"type": "string"}}}}},
                        "blast_radius_groups": {"type": "array", "description": "Dependency and risk groups in ascending severity (none, low, medium, high, blocked). Severity is driven by recorded dependency paths and missing-evidence counts; ambiguous, rejected, or safety-gated items are grouped as blocked regardless of footprint.", "items": {"type": "object", "properties": {"id": {"type": "string"}, "severity": {"type": "string"}, "reason": {"type": "string"}, "item_ids": {"type": "array", "items": {"type": "string"}}}}},
                        "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                        "limitations": {"type": "array", "items": {"type": "string"}},
                        "non_goals": {"type": "array", "items": {"type": "string"}}
                      }
                    },
                    "plan_truth_level": {"type": "string"},
                    "items_count": {"type": "integer"},
                    "ready_import_count": {"type": "integer"},
                    "refused_import_count": {"type": "integer"},
                    "wave_summaries": {"type": "array", "description": "Per-wave item counts in staging order.", "items": {"type": "object", "properties": {"wave_id": {"type": "string"}, "order": {"type": "integer"}, "item_count": {"type": "integer"}}}},
                    "blast_radius_summaries": {"type": "array", "description": "Per-blast-radius-group item counts in ascending severity order.", "items": {"type": "object", "properties": {"group_id": {"type": "string"}, "severity": {"type": "string"}, "item_count": {"type": "integer"}}}},
                    "total_findings_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"]},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "truth_basis": {"type": "string"},
                    "analysis_status": {"type": "string"},
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
