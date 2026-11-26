// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

const deploymentConfigInfluence = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Deployment configuration influence story",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string", "description": "Canonical service name from buildDeploymentConfigInfluenceResponse."},
                    "workload_id": {"type": "string", "description": "Canonical workload id from buildDeploymentConfigInfluenceResponse."},
                    "environment": {"type": "string", "description": "Environment scope echoed from the request by buildDeploymentConfigInfluenceResponse."},
                    "subject": {"type": "object", "description": "Influence subject from deploymentConfigSubject: {type service, id, name, environment}.", "required": ["type", "id", "name", "environment"], "properties": {"type": {"type": "string", "description": "Subject kind; always service."}, "id": {"type": "string", "description": "Canonical workload id."}, "name": {"type": "string", "description": "Canonical service name."}, "environment": {"type": "string", "description": "Environment scope."}}},
                    "story": {"type": "string", "description": "Deployment configuration influence story from deploymentConfigStory with values, image, runtime, resource, and target counts."},
                    "influencing_repositories": {"type": "array", "description": "Influencing repositories from deploymentConfigInfluencingRepositories: the service owner, deployment sources, and configuration artifact repos with roles.", "items": {"type": "object", "description": "Influencing repository rows: repo_id, repo_name, and roles.", "required": ["repo_id", "repo_name", "roles"], "properties": {"repo_id": {"type": "string", "description": "Canonical repository id."}, "repo_name": {"type": "string", "description": "Display name of the repository."}, "roles": {"type": "array", "description": "Influence roles: service_owner, deployment_source, and/or configuration_artifact.", "items": {"type": "string"}}}}},
                    "values_layers": {"type": "array", "description": "Values-layer artifact rows from filterDeploymentConfigRows: portable rows (artifact_family, evidence_kind, matched_alias, matched_value, environment, resolved_id, repo_id, repo_name, relative_path, start/end lines) present only when observed.", "items": {"type": "object", "description": "Portable configuration artifact rows from deploymentConfigPortableRow.", "properties": {"artifact_family": {"type": "string", "description": "Artifact family of the row."}, "evidence_kind": {"type": "string", "description": "Evidence kind of the row."}, "matched_alias": {"type": "string", "description": "Matched alias behind the row, when present."}, "matched_value": {"type": "string", "description": "Matched value behind the row, when present."}, "environment": {"type": "string", "description": "Environment of the row, when present."}, "resolved_id": {"type": "string", "description": "Resolved id behind the row, when present."}, "repo_id": {"type": "string", "description": "Repository holding the artifact, when present."}, "repo_name": {"type": "string", "description": "Display name of the holding repository, when present."}, "relative_path": {"type": "string", "description": "Repo-relative path of the artifact, when present."}, "start_line": {"type": "integer", "description": "Start line, present when positive."}, "end_line": {"type": "integer", "description": "End line, present when positive."}}}},
                    "image_tag_sources": {"type": "array", "description": "Image tag source rows: portable artifact rows, or {image_ref, source kubernetes_resource_image} fallback rows from imageReferenceRows when no tag artifacts exist.", "items": {"type": "object", "description": "Image tag source rows.", "properties": {"artifact_family": {"type": "string", "description": "Artifact family of the row."}, "evidence_kind": {"type": "string", "description": "Evidence kind of the row."}, "matched_alias": {"type": "string", "description": "Matched alias, when present."}, "matched_value": {"type": "string", "description": "Matched value, when present."}, "environment": {"type": "string", "description": "Environment, when present."}, "resolved_id": {"type": "string", "description": "Resolved id, when present."}, "repo_id": {"type": "string", "description": "Repository holding the artifact, when present."}, "repo_name": {"type": "string", "description": "Display name, when present."}, "relative_path": {"type": "string", "description": "Repo-relative path, when present."}, "start_line": {"type": "integer", "description": "Start line, present when positive."}, "end_line": {"type": "integer", "description": "End line, present when positive."}, "image_ref": {"type": "string", "description": "Image ref, present on fallback rows."}, "source": {"type": "string", "description": "Row source; kubernetes_resource_image on fallback rows."}}}},
                    "runtime_setting_sources": {"type": "array", "description": "Runtime setting source rows from filterDeploymentConfigRows: portable rows with keys present only when observed.", "items": {"type": "object", "description": "Portable configuration artifact rows.", "properties": {"artifact_family": {"type": "string", "description": "Artifact family of the row."}, "evidence_kind": {"type": "string", "description": "Evidence kind of the row."}, "matched_alias": {"type": "string", "description": "Matched alias, when present."}, "matched_value": {"type": "string", "description": "Matched value, when present."}, "environment": {"type": "string", "description": "Environment, when present."}, "resolved_id": {"type": "string", "description": "Resolved id, when present."}, "repo_id": {"type": "string", "description": "Repository holding the artifact, when present."}, "repo_name": {"type": "string", "description": "Display name, when present."}, "relative_path": {"type": "string", "description": "Repo-relative path, when present."}, "start_line": {"type": "integer", "description": "Start line, present when positive."}, "end_line": {"type": "integer", "description": "End line, present when positive."}}}},
                    "resource_limit_sources": {"type": "array", "description": "Resource limit source rows from filterDeploymentConfigRows: portable rows with keys present only when observed.", "items": {"type": "object", "description": "Portable configuration artifact rows.", "properties": {"artifact_family": {"type": "string", "description": "Artifact family of the row."}, "evidence_kind": {"type": "string", "description": "Evidence kind of the row."}, "matched_alias": {"type": "string", "description": "Matched alias, when present."}, "matched_value": {"type": "string", "description": "Matched value, when present."}, "environment": {"type": "string", "description": "Environment, when present."}, "resolved_id": {"type": "string", "description": "Resolved id, when present."}, "repo_id": {"type": "string", "description": "Repository holding the artifact, when present."}, "repo_name": {"type": "string", "description": "Display name, when present."}, "relative_path": {"type": "string", "description": "Repo-relative path, when present."}, "start_line": {"type": "integer", "description": "Start line, present when positive."}, "end_line": {"type": "integer", "description": "End line, present when positive."}}}},
                    "rendered_targets": {"type": "array", "description": "Rendered target rows from renderedDeploymentConfigTargets: source plus the non-empty id/entity/name/kind/namespace/environment keys.", "items": {"type": "object", "description": "Rendered target rows from deploymentConfigTargetRow.", "required": ["source"], "properties": {"source": {"type": "string", "description": "Target source: kubernetes_resource or controller.", "enum": ["kubernetes_resource", "controller"]}, "id": {"type": "string", "description": "Target id, present when the row carries one."}, "entity_id": {"type": "string", "description": "Entity id, present when the row carries one."}, "name": {"type": "string", "description": "Target name, present when the row carries one."}, "entity_name": {"type": "string", "description": "Entity name, present when the row carries one."}, "kind": {"type": "string", "description": "Target kind, present when the row carries one."}, "namespace": {"type": "string", "description": "Namespace, present when the row carries one."}, "environment": {"type": "string", "description": "Environment, present when the row carries one."}}}},
                    "read_first_files": {"type": "array", "description": "Read-first file handles from deploymentConfigReadFirstFiles: one row per repo and relative path with the next call and evidence kinds.", "items": {"type": "object", "description": "Read-first file rows from deploymentConfigReadFirstFiles.", "required": ["repo_id", "repo_name", "relative_path", "next_call", "reason"], "properties": {"repo_id": {"type": "string", "description": "Repository holding the file."}, "repo_name": {"type": "string", "description": "Display name of the holding repository."}, "relative_path": {"type": "string", "description": "Repo-relative path to read first."}, "next_call": {"type": "string", "description": "Next call for the file; always get_file_lines."}, "reason": {"type": "string", "description": "Why the file is read-first; always configuration influence evidence."}, "start_line": {"type": "integer", "description": "Start line, present when positive."}, "end_line": {"type": "integer", "description": "End line, present when positive."}, "evidence_kinds": {"type": "array", "description": "Evidence kinds behind the file.", "items": {"type": "string"}}}}},
                    "recommended_next_calls": {"type": "array", "description": "Recommended next calls from deploymentConfigNextCalls: trace_deployment_chain, an environment-scoped investigate call, and a get_file_lines call for the first file.", "items": {"type": "string"}},
                    "limitations": {"type": "array", "description": "Limitations from deploymentConfigLimitations for the environment and evidence state.", "items": {"type": "string"}},
                    "deployment_source_limits": {
                      "type": "object",
                      "description": "Bound metadata for deployment_sources carried on the influence response.",
                      "properties": {
                        "limit": {"type": "integer", "description": "Row cap applied to deployment sources."},
                        "query_sentinel_limit": {"type": "integer", "description": "Per-query sentinel bound, including the one-row truncation probe."},
                        "returned_count": {"type": "integer", "description": "Deployment-source rows returned after the cap."},
                        "observed_count": {"type": "integer", "description": "Deployment-source rows observed before the cap."},
                        "observed_count_is_lower_bound": {"type": "boolean", "description": "True when the sentinel bound was reached."},
                        "canonical_observed_count": {"type": "integer", "description": "Canonical rows observed before the cap."},
                        "repository_observed_count": {"type": "integer", "description": "Distinct repositories observed before the cap."},
                        "truncated": {"type": "boolean", "description": "True when rows were cut to the limit."},
                        "ordering": {"type": "array", "description": "Sort key order applied to the rows.", "items": {"type": "string"}}
                      }
                    },
` + K8sResourceLimits + `
                    "coverage": {
                      "type": "object",
                      "description": "Influence coverage from buildDeploymentConfigInfluenceResponse: query shape, limit, truncation, bounds, and handle counts.",
                      "required": ["query_shape", "limit", "truncated", "observed_count_is_lower_bound", "artifact_candidate_count", "deployment_source_count", "rendered_target_count", "environment", "portable_file_handles", "uses_file_content_payloads"],
                      "properties": {
                        "query_shape": {"type": "string", "description": "Read shape; always deployment_config_influence_story."},
                        "limit": {"type": "integer", "description": "Applied row bound."},
                        "truncated": {"type": "boolean", "description": "True when any family read was truncated."},
                        "observed_count_is_lower_bound": {"type": "boolean", "description": "True when an upstream bound makes counts lower bounds."},
                        "artifact_candidate_count": {"type": "integer", "description": "Artifact candidates considered."},
                        "deployment_source_count": {"type": "integer", "description": "Deployment sources considered."},
                        "rendered_target_count": {"type": "integer", "description": "Rendered targets emitted."},
                        "environment": {"type": "string", "description": "Environment scope."},
                        "portable_file_handles": {"type": "integer", "description": "Portable file handles emitted."},
                        "uses_file_content_payloads": {"type": "boolean", "description": "Whether file content payloads are used; always false."}
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": ` + traceDeploymentNotFoundResponse + `,
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },`
