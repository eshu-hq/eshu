// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"github.com/eshu-hq/eshu/go/internal/query/openapi/schema"
)

// Entities is the OpenAPI path fragment documenting the `/api/v0/*` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Entities = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
                    "relationships_complete": {"type": "boolean", "description": "Present and false only when a relationship build is incomplete: either a k8s SELECTS K8sResource candidate scan reached the repository entity limit (issue #5343 follow-up #5367), or a GitHub Actions workflow source_cache reached its 32 KiB cap. Absent when the relationship build completed."},
                    "relationships_truncation_reason": {"type": "string", "description": "Machine-readable reason paired with relationships_complete=false. k8s_resource_candidate_scan_truncated_at_5000 means the k8s SELECTS candidate scan reached its repository limit; github_actions_source_cache_truncated means a GitHub Actions workflow source_cache reached its 32 KiB cap before dependency extraction completed.", "enum": ["k8s_resource_candidate_scan_truncated_at_5000", "github_actions_source_cache_truncated"]},
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
          {"$ref": "#/components/parameters/WorkloadId"},
          {"name": "evidence_detail", "in": "query", "required": false, "schema": {"type": "string", "enum": ["full", "handles"], "default": "full"}, "description": "How deployment_evidence.artifacts and api_surface.endpoints rows are shaped. full (the HTTP default when absent) returns every row in full. handles projects artifact rows to id, relationship_type and resolved_id, endpoint rows to id, path and methods, drops deployment_evidence.evidence_index and the values built from repository content (deployment_artifacts, shared_config_paths, delivery_paths, delivery_family_paths, delivery_family_story, delivery_workflows, topology_story, relationship_overview), keeps every count, and names each reduced family with its total in truth.omissions. The MCP get_workload_context and get_service_context tools default to handles. Unknown values return 400 invalid_argument."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
                    "evidence_boundaries": ` + schema.EvidenceBoundaries + `
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
        "description": "Returns context for a service by name. A name that matches more workloads than the bounded lookup reads returns 409; retry with a workload id.",
        "operationId": "getServiceContext",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {"name": "evidence_detail", "in": "query", "required": false, "schema": {"type": "string", "enum": ["full", "handles"], "default": "full"}, "description": "How deployment_evidence.artifacts and api_surface.endpoints rows are shaped. full (the HTTP default when absent) returns every row in full. handles projects artifact rows to id, relationship_type and resolved_id, endpoint rows to id, path and methods, drops deployment_evidence.evidence_index and the values built from repository content (deployment_artifacts, shared_config_paths, delivery_paths, delivery_family_paths, delivery_family_story, delivery_workflows, topology_story, relationship_overview), keeps every count, and names each reduced family with its total in truth.omissions. The MCP get_workload_context and get_service_context tools default to handles. Unknown values return 400 invalid_argument."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
          "409": {"$ref": "#/components/responses/Conflict"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/services/{service_name}/story": {
      "get": {
        "tags": ["entities"],
        "summary": "Get service story",
        "description": "Returns the one-call service dossier for the service. An ambiguous selector answers 409 with error.code ambiguous; each candidate in error.details.candidates carries next_selector, the argument (service_id) and value to pass next. A candidate with legacy_id true is a leftover name-only workload from the identity change; the scoped candidate is the live workload.",
        "operationId": "getServiceStory",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {"name": "service_id", "in": "query", "description": "Exact workload or service id selector. When supplied, the route resolves this id before service-name candidates.", "schema": {"type": "string"}},
          {"name": "repo", "in": "query", "description": "Repository selector that separates only services in different repositories; two same-named services in one repository stay ambiguous and need service_id. Accepts canonical repository id, repository name, slug, path, or remote URL.", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "description": "Runtime environment selector used to disambiguate duplicate service names by workload instance environment.", "schema": {"type": "string"}},
          {"name": "evidence_detail", "in": "query", "required": false, "schema": {"type": "string", "enum": ["full", "handles"], "default": "full"}, "description": "How the evidence lists are shaped. full (the HTTP default when absent) returns every list in full. handles keeps every section, count, and top-level status, truth, and freshness field, and projects the bulky lists to identity rows that keep their row-level truth labels (including confidence, relationship_types, consumer_kinds, evidence_kinds, relationship_basis, resolution_mode, candidate_status, missing_relationship, service_anchor_status, and service_anchor_reason; an artifact row keeps resolved_id, the key get_relationship_evidence takes for the rest): deployment_evidence.artifacts and api_surface.endpoints (id, relationship_type, resolved_id and id, path, methods), code_to_runtime_trace segment evidence (each segment keeps its own identity keys, so the image_package digest and SBOM identity ship; a preview of 10 handles where the rows also ship in another section, with truncated, evidence_shipped, and evidence_omitted), upstream_dependencies, hostnames, entrypoints, entrypoint_candidates, network_paths, dependents, consumer_repositories, provisioning_source_chains, cloud_resources, uncorrelated_cloud_resources, investigation repositories, and evidence_graph edges. It drops the content-derived deployment_evidence values, the downstream_consumers row lists (named by graph_dependents_shipped_at and content_consumers_shipped_at), and the evidence_graph nodes no shipped edge touches (node_count, nodes_truncated, nodes_omitted). truth.omissions lists each reduced family with its total. Each total is the story's own pre-cap count and may be a lower bound, because the story carries a flag for only some of the bounds its reads apply; treat a total as exact only for the families the Context evidence budget reference lists as exact. investigation.repositories_with_evidence reports the rows it shipped (at most 50), so it is also a lower bound when repositories_considered ends with the __truncated__ marker. The MCP get_service_story tool defaults to handles. Unknown values return 400 invalid_argument."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
                    "ci_cd_evidence": {"type": "object", "description": "Repository-scoped static workflow, live run, and artifact evidence. A file read that returns a sentinel row past the 5,000-file limit adds static_workflow_artifacts.candidate_pool_status=unknown_at_limit and missing_evidence class static_workflow_coverage_unknown. A repository with exactly 5,000 files is a complete scan and adds neither. Zero observed workflows on a truncated scan means state=unknown; positive counts describe the observed page. Uncapped serialization is preserved."},
                    "service_identity": {"type": "object"},
                    "api_surface": {"type": "object", "description": "The API surface, shipped once. deployment_overview.api_surface keeps the counts and names this key in endpoints_shipped_at."},
                    "evidence_detail": {"type": "string", "enum": ["full", "handles"], "description": "The evidence_detail the response was shaped with."},
                    "evidence_detail_drilldown": {"type": "object", "additionalProperties": true, "description": "Present only under handles when a family was reduced: how to fetch the full rows."},
                    "deployment_lanes": {"type": "array", "items": {"type": "object"}},
                    "upstream_dependencies": {"type": "array", "items": {"type": "object"}},
                    "downstream_consumers": {"type": "object"},
                    "evidence_graph": {"type": "object"},
                    "result_limits": {"type": "object", "description": "Bounds of the dossier lists. truncated is true when a rendered list was capped, an upstream read was truncated, the infrastructure read landed past its bound, or a deployment-artifact bound named in limitations bit."},
                    "investigation": {"type": "object"},
                    "deployment_overview": {"type": "object"},
                    "limitations": {"type": "array", "description": "Explicit reasons the service evidence is partial, present only when one applies. deployment_artifacts_candidates_budget_reached means the request budget of 12,000 config-artifact candidates beyond the per-repository floor of 50 ran out, so the deployment_evidence config rows of at least one related repository are a lower bound; deployment_artifacts_related_file_list_truncated_at_5000 means a related repository has more than 5,000 files, so its candidates past the window are unknown; deployment_artifacts_anchor_file_list_truncated_at_5000 means the service repository itself has more than 5,000 files, so the deployment evidence read from its file list is a lower bound. Each folds into result_limits.truncated and answer_metadata.truncated.", "items": {"type": "string"}},
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
                    "evidence_boundaries": ` + schema.EvidenceBoundaries + `
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
