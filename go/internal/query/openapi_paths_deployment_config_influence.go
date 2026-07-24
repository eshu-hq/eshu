// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathDeploymentConfigInfluence = `
    "/api/v0/impact/deployment-config-influence": {
      "post": {
        "tags": ["impact"],
        "summary": "Investigate deployment configuration influence",
        "description": "Returns a bounded service deployment configuration story with influencing repositories, values layers, image tag sources, runtime setting sources, resource limit sources, rendered targets, and portable file handles. Scoped tokens receive the same shape; a service outside the caller's grant 404s and cross-repository evidence outside the grant is withheld.",
        "operationId": "investigateDeploymentConfigInfluence",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Provide service_name or workload_id.",
                "anyOf": [
                  {"required": ["service_name"]},
                  {"required": ["workload_id"]}
                ],
                "properties": {
                  "service_name": {"type": "string", "description": "Service name to investigate"},
                  "workload_id": {"type": "string", "description": "Canonical workload id to investigate"},
                  "environment": {"type": "string", "description": "Optional environment scope"},
                  "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Deployment configuration influence story",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string"},
                    "workload_id": {"type": "string"},
                    "environment": {"type": "string"},
                    "subject": {"type": "object"},
                    "story": {"type": "string"},
                    "influencing_repositories": {"type": "array", "items": {"type": "object"}},
                    "values_layers": {"type": "array", "items": {"type": "object"}},
                    "image_tag_sources": {"type": "array", "items": {"type": "object"}},
                    "runtime_setting_sources": {"type": "array", "items": {"type": "object"}},
                    "resource_limit_sources": {"type": "array", "items": {"type": "object"}},
                    "rendered_targets": {"type": "array", "items": {"type": "object"}},
                    "read_first_files": {"type": "array", "items": {"type": "object"}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "string"}},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "deployment_source_limits": {
                      "type": "object",
                      "properties": {
                        "limit": {"type": "integer"},
                        "query_sentinel_limit": {"type": "integer"},
                        "returned_count": {"type": "integer"},
                        "observed_count": {"type": "integer"},
                        "observed_count_is_lower_bound": {"type": "boolean"},
                        "canonical_observed_count": {"type": "integer"},
                        "repository_observed_count": {"type": "integer"},
                        "truncated": {"type": "boolean"},
                        "ordering": {"type": "array", "items": {"type": "string"}}
                      }
                    },
` + openAPIImpactK8sResourceLimits + `
                    "coverage": {
                      "type": "object",
                      "properties": {
                        "query_shape": {"type": "string"},
                        "limit": {"type": "integer"},
                        "truncated": {"type": "boolean"},
                        "observed_count_is_lower_bound": {"type": "boolean"},
                        "artifact_candidate_count": {"type": "integer"},
                        "deployment_source_count": {"type": "integer"},
                        "rendered_target_count": {"type": "integer"},
                        "environment": {"type": "string"},
                        "portable_file_handles": {"type": "integer"},
                        "uses_file_content_payloads": {"type": "boolean"}
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },`
