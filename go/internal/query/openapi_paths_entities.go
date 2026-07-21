// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsEntities = `
    "/api/v0/entities/resolve": {
      "post": {
        "tags": ["entities"],
        "summary": "Resolve entity",
        "description": "Resolves an entity by exact case-sensitive name. Requests without repo_id require a supported type and use the current content entity index; repository, directory, and file types require repo_id. Canonical content-entity IDs and workload resolution keep their dedicated exact paths.",
        "operationId": "resolveEntity",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["name"],
                "properties": {
                  "name": {"type": "string", "description": "Entity name to search for"},
                  "type": {"type": "string", "description": "Entity type filter. Required when repo_id is omitted, except for canonical content-entity IDs. Unknown types fail closed."},
                  "repo_id": {"type": "string", "description": "Optional repository ID filter"},
                  "limit": {"type": "integer", "default": 10, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Resolved entities",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "entities": {
                      "type": "array",
                      "items": {"$ref": "#/components/schemas/EntityRef"}
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/entities/{entity_id}/context": {
      "get": {
        "tags": ["entities"],
        "summary": "Get entity context",
        "description": "Returns context and relationships for a specific entity.",
        "operationId": "getEntityContext",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/EntityId"}
        ],
        "responses": {
          "200": {
            "description": "Entity context",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": {"type": "string"},
                    "labels": {"type": "array", "items": {"type": "string"}},
                    "name": {"type": "string"},
                    "file_path": {"type": "string"},
                    "language": {"type": "string"},
                    "start_line": {"type": "integer"},
                    "end_line": {"type": "integer"},
                    "metadata": {"type": "object", "additionalProperties": true},
                    "semantic_summary": {"type": "string"},
                    "semantic_profile": {"type": "object", "additionalProperties": true},
                    "story": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "repo_name": {"type": "string"},
                    "relationships": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}},
                    "relationships_complete": {"type": "boolean", "description": "Present and false only when a k8s SELECTS relationship build's K8sResource candidate scan was truncated at the repository entity limit (issue #5343 follow-up #5367); absent when the scan completed, so existing responses stay unchanged."},
                    "relationships_truncation_reason": {"type": "string", "description": "Machine-readable reason paired with relationships_complete=false. Currently one value: k8s_resource_candidate_scan_truncated_at_5000.", "enum": ["k8s_resource_candidate_scan_truncated_at_5000"]},
                    "result_limits": {"type": "object", "description": "Additive drilldown block: bounded relationship limit, deterministic ordering, relationship count, truncation flag, and the get_relationship_evidence drilldown plus context path.", "additionalProperties": true},
                    "partial_reasons": {"type": "array", "description": "Explicit limitations or unsupported-evidence reasons for the entity context read; always present so the envelope shape is stable.", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/workloads/{workload_id}/context": {
      "get": {
        "tags": ["entities"],
        "summary": "Get workload context",
        "description": "Returns context and deployment instances for a workload.",
        "operationId": "getWorkloadContext",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/WorkloadId"}
        ],
        "responses": {
          "200": {
            "description": "Workload context",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/WorkloadContext"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/workloads/{workload_id}/story": {
      "get": {
        "tags": ["entities"],
        "summary": "Get workload story",
        "description": "Returns a narrative summary for the workload.",
        "operationId": "getWorkloadStory",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/WorkloadId"}
        ],
        "responses": {
          "200": {
            "description": "Workload narrative",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "workload_id": {"type": "string"},
                    "name": {"type": "string"},
                    "story": {"type": "string"},
                    "result_limits": {"type": "object", "description": "Additive drilldown block: bounded limit, deterministic ordering, instance/dependent/consumer counts, truncation flag, and the get_workload_context drilldown plus context path.", "additionalProperties": true},
                    "partial_reasons": {"type": "array", "description": "Explicit limitations or unsupported-evidence reasons for the workload story read; always present so the envelope shape is stable.", "items": {"type": "string"}},
                    "evidence_boundaries": ` + openAPIEvidenceBoundariesSchema + `
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/services/{service_name}/context": {
      "get": {
        "tags": ["entities"],
        "summary": "Get service context",
        "description": "Returns context for a service by name.",
        "operationId": "getServiceContext",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"}
        ],
        "responses": {
          "200": {
            "description": "Service context",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/WorkloadContext"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/services/{service_name}/story": {
      "get": {
        "tags": ["entities"],
        "summary": "Get service story",
        "description": "Returns the one-call service dossier for the service.",
        "operationId": "getServiceStory",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {"name": "service_id", "in": "query", "description": "Exact workload or service id selector. When supplied, the route resolves this id before service-name candidates.", "schema": {"type": "string"}},
          {"name": "repo", "in": "query", "description": "Repository selector used to disambiguate duplicate service names. Accepts canonical repository id, repository name, slug, path, or remote URL.", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "description": "Runtime environment selector used to disambiguate duplicate service names by workload instance environment.", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Service narrative",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string"},
                    "story": {"type": "string"},
                    "story_sections": {"type": "array", "items": {"type": "object"}},
                    "code_to_runtime_trace": {"type": "object"},
                    "service_identity": {"type": "object"},
                    "api_surface": {"type": "object"},
                    "deployment_lanes": {"type": "array", "items": {"type": "object"}},
                    "upstream_dependencies": {"type": "array", "items": {"type": "object"}},
                    "downstream_consumers": {"type": "object"},
                    "evidence_graph": {"type": "object"},
                    "result_limits": {"type": "object"},
                    "investigation": {"type": "object"},
                    "deployment_overview": {"type": "object"},
                    "hostnames": {"type": "array", "items": {"type": "object"}},
                    "entrypoint_candidates": {"type": "array", "description": "Dotted hostname-shaped candidates kept as non-entrypoint supporting evidence when rejected as config keys or field paths, or when ambiguous without stronger hostname evidence.", "items": {"type": "object"}},
                    "entrypoints": {"type": "array", "items": {"type": "object"}},
                    "network_paths": {"type": "array", "items": {"type": "object"}},
                    "ingress_posture": {"type": "object", "description": "WAF coverage and TLS termination posture for the service's internet-facing edge resources, derived strictly from the materialized AWS_wafv2_web_acl_protects_resource and AWS_acm_certificate_used_by_resource edges. waf_coverage and tls_termination are three-valued (protected/unprotected/unproven and terminated/not_terminated/unproven) so an observed-negative is never confused with missing evidence."},
                    "observed_config_environments": {"type": "array", "items": {"type": "string"}},
                    "dependents": {"type": "array", "items": {"type": "object"}},
                    "consumer_repositories": {"type": "array", "items": {"type": "object"}},
                    "provisioning_source_chains": {"type": "array", "items": {"type": "object"}},
                    "cloud_resources": {"type": "array", "description": "CloudResource dependencies admitted only from materialized WorkloadInstance USES CloudResource relationships.", "items": {"type": "object"}},
                    "uncorrelated_cloud_resources": {"type": "array", "description": "CloudResource candidates that match the service name or cloud identifier but remain missing the reducer-owned workload-to-cloud relationship required for dependency truth.", "items": {"type": "object"}},
                    "uncorrelated_cloud_resources_truncated": {"type": "boolean", "description": "Present and true when the uncorrelated_cloud_resources list was capped at the dossier item limit and additional candidates exist in the graph."},
                    "deployment_evidence": {
                      "type": "object",
                      "description": "Deployment, CI, and environment evidence pointers. Artifacts include source_location plus resolved_id/generation_id for Postgres evidence drilldown; evidence_index groups those pointers by relationship type, artifact family, and evidence kind."
                    },
                    "documentation_overview": {"type": "object"},
                    "support_overview": {"type": "object"},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer metadata with schema_version, evidence_handles, missing_evidence, limitations, truncated, coverage, partial_reasons, and recommended_next_calls."},
                    "evidence_boundaries": ` + openAPIEvidenceBoundariesSchema + `
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
