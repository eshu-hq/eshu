// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// Context documents the repository context route. It is split from Routes
// to keep that file under the source size cap.
const Context = `
    "/api/v0/repositories/{repo_id}/context": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository context",
        "description": "Returns repository metadata with graph statistics. File count and languages use a content-files-only summary when available. A file-derived infrastructure overview beyond 5,000 files is explicitly marked partial. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryContext",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"},
          {"name": "evidence_detail", "in": "query", "required": false, "schema": {"type": "string", "enum": ["full", "handles"], "default": "full"}, "description": "How the context row lists are shaped. full (the HTTP default when absent) returns every list in full. handles projects relationships, relationship_overview.relationships, consumers, api_surface.endpoints, deployment_evidence.artifacts, deployment_artifacts, infrastructure_overview.deployment_artifacts, entry_points, and infrastructure to identity rows, drops the repeated relationship partition lists and the evidence_index regrouping with a pointer, keeps every count, and names each reduced family with its total in truth.omissions. The MCP get_repo_context tool defaults to handles. Unknown values return 400 invalid_argument."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Repository context",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef", "description": "Repository identity emitted by the repository context handler from the base row (component shape)."},
                    "file_count": {"type": "integer", "description": "Indexed file count emitted by the repository context handler from the summary counts."},
                    "workload_count": {"type": "integer", "description": "Workload count emitted by the repository context handler from the summary counts."},
                    "platform_count": {"type": "integer", "description": "Platform count emitted by the repository context handler from the summary counts."},
                    "dependency_count": {"type": "integer", "description": "Dependency count emitted by the repository context handler from the summary counts."},
                    "relationships": {
                      "type": "array",
                      "description": "Outgoing repository relationships, emitted by the repository context handler from repository.QueryRepoDependencies (or the relationship read model) as graph-edge rows with evidence pointers.",
                      "items": {
                        "type": "object",
                        "description": "One outgoing repository relationship row. Owner: repository.QueryRepoDependencies.",
                        "properties": {
                          "type": {"type": "string", "description": "Edge type emitted by the repository relationship row projection."},
                          "target_name": {"type": "string", "description": "Target repository name emitted by the repository relationship row projection."},
                          "target_id": {"type": "string", "description": "Target repository identity emitted by the repository relationship row projection."},
                          "direction": {"type": "string", "description": "Edge direction emitted by the repository relationship row projection (present on read-model rows)."},
                          "source_name": {"type": "string", "description": "Source name emitted by the repository relationship row projection (present on read-model rows)."},
                          "source_id": {"type": "string", "description": "Source identity emitted by the repository relationship row projection (present on read-model rows)."},
                          "evidence_type": {"type": "string", "description": "Edge evidence type emitted by the repository relationship row projection (present when the edge carries one)."},
                          "resolved_id": {"type": "string", "description": "Resolved evidence identity copied by repository.copyRelationshipEvidenceMetadata (present when the edge carries one)."},
                          "generation_id": {"type": "string", "description": "Evidence generation copied by repository.copyRelationshipEvidenceMetadata (present when the edge carries one)."},
                          "confidence": {"type": "number", "description": "Edge confidence copied by repository.copyRelationshipEvidenceMetadata (present when positive)."},
                          "evidence_count": {"type": "integer", "description": "Evidence atom count copied by repository.copyRelationshipEvidenceMetadata (present when positive)."},
                          "evidence_kinds": {"type": "array", "items": {"type": "string"}, "description": "Evidence kinds copied by repository.copyRelationshipEvidenceMetadata (present when non-empty)."},
                          "resolution_source": {"type": "string", "description": "Resolution source copied by repository.copyRelationshipEvidenceMetadata (present when the edge carries one)."},
                          "confidence_basis": {"type": "string", "description": "Confidence basis copied by repository.copyRelationshipEvidenceMetadata (present when the edge carries one)."},
                          "rationale": {"type": "string", "description": "Edge rationale emitted by the repository relationship row projection (present when the edge carries one)."}
                        },
                        "required": ["type", "target_name", "target_id"]
                      }
                    },
                    "relationship_overview": {
                      "type": "object",
                      "description": "Incoming and outgoing typed repository relationships with lightweight evidence pointers, emitted by the repository context handler from repository.BuildRepositoryRelationshipOverview.",
                      "properties": {
                        "relationship_count": {"type": "integer", "description": "Relationships in the overview, emitted by repository.BuildRepositoryRelationshipOverview as len(rows)."},
                        "relationships": {
                          "type": "array",
                          "description": "Normalized relationship rows emitted by repository.BuildRepositoryRelationshipOverview.",
                          "items": {
                            "type": "object",
                            "description": "One normalized relationship row. Owner: repository.BuildRepositoryRelationshipOverview.",
                            "properties": {
                              "type": {"type": "string", "description": "Edge type emitted by repository.BuildRepositoryRelationshipOverview."},
                              "target_name": {"type": "string", "description": "Target name emitted by repository.BuildRepositoryRelationshipOverview."},
                              "target_id": {"type": "string", "description": "Target identity emitted by repository.BuildRepositoryRelationshipOverview."},
                              "direction": {"type": "string", "description": "Edge direction emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."},
                              "source_name": {"type": "string", "description": "Source name emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."},
                              "source_id": {"type": "string", "description": "Source identity emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."},
                              "evidence_type": {"type": "string", "description": "Edge evidence type emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."},
                              "resolved_id": {"type": "string", "description": "Resolved evidence identity copied by repository.copyRelationshipEvidenceMetadata (present when the row carries one)."},
                              "generation_id": {"type": "string", "description": "Evidence generation copied by repository.copyRelationshipEvidenceMetadata (present when the row carries one)."},
                              "confidence": {"type": "number", "description": "Edge confidence copied by repository.copyRelationshipEvidenceMetadata (present when positive)."},
                              "evidence_count": {"type": "integer", "description": "Evidence atom count copied by repository.copyRelationshipEvidenceMetadata (present when positive)."},
                              "evidence_kinds": {"type": "array", "items": {"type": "string"}, "description": "Evidence kinds copied by repository.copyRelationshipEvidenceMetadata (present when non-empty)."},
                              "resolution_source": {"type": "string", "description": "Resolution source copied by repository.copyRelationshipEvidenceMetadata (present when the row carries one)."},
                          "confidence_basis": {"type": "string", "description": "Confidence basis copied by repository.copyRelationshipEvidenceMetadata (present when the row carries one)."},
                              "rationale": {"type": "string", "description": "Edge rationale emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."}
                            },
                            "required": ["type", "target_name", "target_id"]
                          }
                        },
                        "relationship_types": {"type": "array", "items": {"type": "string"}, "description": "Distinct edge types in the overview, emitted by repository.BuildRepositoryRelationshipOverview."},
                        "evidence_types": {"type": "array", "items": {"type": "string"}, "description": "Distinct evidence types in the overview, emitted by repository.BuildRepositoryRelationshipOverview."},
                        "controller_driven": {"type": "array", "description": "Controller-driven normalized relationship rows, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty). Same row shape as relationships.", "items": {"type": "object", "description": "One controller-driven relationship row. Owner: repository.BuildRepositoryRelationshipOverview.", "properties": {
                          "type": {"type": "string", "description": "Edge type emitted by repository.BuildRepositoryRelationshipOverview."},
                          "target_name": {"type": "string", "description": "Target name emitted by repository.BuildRepositoryRelationshipOverview."},
                          "target_id": {"type": "string", "description": "Target identity emitted by repository.BuildRepositoryRelationshipOverview."},
                          "direction": {"type": "string", "description": "Edge direction emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."},
                          "source_name": {"type": "string", "description": "Source name emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."},
                          "source_id": {"type": "string", "description": "Source identity emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."},
                          "evidence_type": {"type": "string", "description": "Edge evidence type emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."},
                          "resolved_id": {"type": "string", "description": "Resolved evidence identity copied by repository.copyRelationshipEvidenceMetadata (present when the row carries one)."},
                          "generation_id": {"type": "string", "description": "Evidence generation copied by repository.copyRelationshipEvidenceMetadata (present when the row carries one)."},
                          "confidence": {"type": "number", "description": "Edge confidence copied by repository.copyRelationshipEvidenceMetadata (present when positive)."},
                          "evidence_count": {"type": "integer", "description": "Evidence atom count copied by repository.copyRelationshipEvidenceMetadata (present when positive)."},
                          "evidence_kinds": {"type": "array", "items": {"type": "string"}, "description": "Evidence kinds copied by repository.copyRelationshipEvidenceMetadata (present when non-empty)."},
                          "resolution_source": {"type": "string", "description": "Resolution source copied by repository.copyRelationshipEvidenceMetadata (present when the row carries one)."},
                          "confidence_basis": {"type": "string", "description": "Confidence basis copied by repository.copyRelationshipEvidenceMetadata (present when the row carries one)."},
                          "rationale": {"type": "string", "description": "Edge rationale emitted by repository.BuildRepositoryRelationshipOverview (present when the row carries one)."}
                        }, "required": ["type", "target_name", "target_id"]}},
                        "workflow_driven": {"type": "array", "description": "Workflow-driven normalized relationship rows, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty). Same row shape as controller_driven.", "items": {"type": "object", "description": "One workflow-driven relationship row (open map). Owner: repository.BuildRepositoryRelationshipOverview.", "additionalProperties": true}},
                        "iac_driven": {"type": "array", "description": "IaC-driven normalized relationship rows, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty). Same row shape as controller_driven.", "items": {"type": "object", "description": "One IaC-driven relationship row (open map). Owner: repository.BuildRepositoryRelationshipOverview.", "additionalProperties": true}},
                        "other_relationships": {"type": "array", "description": "Remaining normalized relationship rows, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty). Same row shape as controller_driven.", "items": {"type": "object", "description": "One remaining relationship row (open map). Owner: repository.BuildRepositoryRelationshipOverview.", "additionalProperties": true}},
                        "story": {"type": "string", "description": "One-line relationship narrative, emitted by repository.BuildRepositoryRelationshipOverview (present when non-empty)."}
                      },
                      "required": ["relationship_count", "relationships", "relationship_types", "evidence_types"]
                    },
                    "api_surface": {
                      "type": "object",
                      "description": "Graph-backed API endpoint surface exposed by this repository, emitted by the repository context handler from repository.QueryRepoAPISurface via buildGraphAPISurface (present only when endpoints exist).",
                      "properties": {
                        "truth_basis": {"type": "string", "description": "Truth basis emitted by repository.buildGraphAPISurface (always graph)."},
                        "endpoint_count": {"type": "integer", "description": "True endpoint total emitted by repository.buildGraphAPISurface."},
                        "method_count": {"type": "integer", "description": "HTTP method count emitted by repository.buildGraphAPISurface."},
                        "operation_id_count": {"type": "integer", "description": "Operation id count emitted by repository.buildGraphAPISurface."},
                        "endpoints": {
                          "type": "array",
                          "description": "Bounded endpoint rows emitted by repository.buildGraphAPISurface.",
                          "items": {
                            "type": "object",
                            "description": "One bounded endpoint row. Owner: repository.buildGraphAPISurface.",
                            "properties": {
                              "id": {"type": "string", "description": "Endpoint identity emitted by repository.buildGraphAPISurface."},
                              "path": {"type": "string", "description": "Endpoint path emitted by repository.buildGraphAPISurface."},
                              "methods": {"type": "array", "items": {"type": "string"}, "description": "HTTP methods emitted by repository.buildGraphAPISurface."},
                              "operation_ids": {"type": "array", "items": {"type": "string"}, "description": "Operation ids emitted by repository.buildGraphAPISurface."},
                              "source": {"type": "string", "description": "Endpoint source emitted by repository.buildGraphAPISurface (always graph)."},
                              "source_kinds": {"type": "array", "items": {"type": "string"}, "description": "Parser source kinds emitted by repository.buildGraphAPISurface."},
                              "source_paths": {"type": "array", "items": {"type": "string"}, "description": "Source paths emitted by repository.buildGraphAPISurface."},
                              "evidence_source": {"type": "string", "description": "Evidence source emitted by repository.buildGraphAPISurface."},
                              "workload_id": {"type": "string", "description": "Owning workload identity emitted by repository.buildGraphAPISurface (present when the endpoint carries one)."},
                              "workload_name": {"type": "string", "description": "Owning workload name emitted by repository.buildGraphAPISurface (present when the endpoint carries one)."}
                            },
                            "required": ["id", "path", "methods", "operation_ids", "source", "source_kinds", "source_paths", "evidence_source"]
                          }
                        },
                        "source_paths": {"type": "array", "items": {"type": "string"}, "description": "Distinct source paths emitted by repository.buildGraphAPISurface."},
                        "spec_paths": {"type": "array", "items": {"type": "string"}, "description": "Distinct spec paths emitted by repository.buildGraphAPISurface."},
                        "spec_versions": {"type": "array", "items": {"type": "string"}, "description": "Distinct spec versions emitted by repository.buildGraphAPISurface."},
                        "api_versions": {"type": "array", "items": {"type": "string"}, "description": "Distinct API versions emitted by repository.buildGraphAPISurface."},
                        "detail_limit": {"type": "integer", "description": "Endpoint detail bound emitted by repository.buildGraphAPISurface."},
                        "detail_truncated": {"type": "boolean", "description": "True when endpoints past the detail bound exist, emitted by repository.buildGraphAPISurface."},
                        "frameworks": {"type": "array", "items": {"type": "string"}, "description": "Parser frameworks extracted from source kinds, emitted by repository.buildGraphAPISurface (present when non-empty)."},
                        "framework_route_count": {"type": "integer", "description": "Framework endpoint row count emitted by repository.buildGraphAPISurface (present alongside frameworks)."}
                      },
                      "required": ["truth_basis", "endpoint_count", "method_count", "operation_id_count", "endpoints", "source_paths", "spec_paths", "spec_versions", "api_versions", "detail_limit", "detail_truncated"]
                    },
                    "deployment_evidence": {
                      "type": "object",
                      "additionalProperties": true,
                      "description": "Deployment, CI, and environment evidence rows keyed by evidence family, emitted by the repository context handler from queryRepoDeploymentEvidence. Keys are the evidence family names (e.g. artifacts, evidence_index); artifacts carry source_location plus resolved_id/generation_id for Postgres evidence drilldown and evidence_index groups those pointers by relationship type, artifact family, and evidence kind."
                    },
                    "consumers": {
                      "type": "array",
                      "description": "Repositories with incoming relationships to this repository, emitted by the repository context handler from queryRepoConsumers (or the relationship read model) as graph-edge rows with evidence pointers.",
                      "items": {
                        "type": "object",
                        "description": "One incoming-relationship repository row. Owner: repository queryRepoConsumers.",
                        "properties": {
                          "type": {"type": "string", "description": "Edge type emitted by the repository consumer row projection."},
                          "target_name": {"type": "string", "description": "Consumer repository name emitted by the repository consumer row projection."},
                          "target_id": {"type": "string", "description": "Consumer repository identity emitted by the repository consumer row projection."},
                          "direction": {"type": "string", "description": "Edge direction emitted by the repository consumer row projection (present on read-model rows)."},
                          "source_name": {"type": "string", "description": "Source name emitted by the repository consumer row projection (present on read-model rows)."},
                          "source_id": {"type": "string", "description": "Source identity emitted by the repository consumer row projection (present on read-model rows)."},
                          "evidence_type": {"type": "string", "description": "Edge evidence type emitted by the repository consumer row projection (present when the edge carries one)."},
                          "resolved_id": {"type": "string", "description": "Resolved evidence identity copied by repository.copyRelationshipEvidenceMetadata (present when the edge carries one)."},
                          "generation_id": {"type": "string", "description": "Evidence generation copied by repository.copyRelationshipEvidenceMetadata (present when the edge carries one)."},
                          "confidence": {"type": "number", "description": "Edge confidence copied by repository.copyRelationshipEvidenceMetadata (present when positive)."},
                          "evidence_count": {"type": "integer", "description": "Evidence atom count copied by repository.copyRelationshipEvidenceMetadata (present when positive)."},
                          "evidence_kinds": {"type": "array", "items": {"type": "string"}, "description": "Evidence kinds copied by repository.copyRelationshipEvidenceMetadata (present when non-empty)."},
                          "resolution_source": {"type": "string", "description": "Resolution source copied by repository.copyRelationshipEvidenceMetadata (present when the edge carries one)."},
                          "confidence_basis": {"type": "string", "description": "Confidence basis copied by repository.copyRelationshipEvidenceMetadata (present when the edge carries one)."},
                          "rationale": {"type": "string", "description": "Edge rationale emitted by the repository consumer row projection (present when the edge carries one)."}
                        },
                        "required": ["type", "target_name", "target_id"]
                      }
                    },
                    "language_breakdown": {
                      "type": "object",
                      "description": "Per-language file counts for this repository, derived from indexed File nodes. Omitted when no language data is available. Keys are language names (e.g. go, python, yaml); values are integer file counts.",
                      "additionalProperties": {"type": "integer"}
                    },
                    "source_tool_breakdown": {
                      "type": "object",
                      "description": "Per-source_tool outgoing relationship-edge counts for this repository. Omitted when no edges carry a source_tool property. Keys are canonical source_tool tokens (e.g. terraform, helm, ansible); values are integer edge counts.",
                      "additionalProperties": {"type": "integer"}
                    },
                    "partial_reasons": {
                      "type": "array",
                      "description": "Explicit reasons the response is partial, e.g. infrastructure_read_degraded when the auxiliary infrastructure graph read failed but the rest of the context still answers 200, relationships_read_degraded or consumers_read_degraded when one of the other auxiliary graph reads failed (each read has its own <read>_read_degraded value), or infrastructure_truncated when a healthy infrastructure read landed past its bound. repository_context_file_read_truncated_at_5000 means file-derived infrastructure and deployment-artifact overviews use only the first 5,000 files; repository_context_file_read_degraded means that file list failed and those overviews may be omitted; repository_context_content_coverage_degraded means the content-file summary failed and graph file counts/languages were used. deployment_artifacts_read_degraded means a content or graph read of the deployment-artifact stage failed, so deployment_artifacts and infrastructure_overview hold the artifacts from the loads that succeeded and lack the artifacts of the load that failed. deployment_artifacts_candidates_budget_reached means the request budget of 12,000 config-artifact candidates beyond the per-repository floor of 50 ran out, so at least one related repository had candidates inside its 5,000-file window that were not read and the config rows are a lower bound; deployment_artifacts_related_file_list_truncated_at_5000 means at least one related repository has more than 5,000 files, so its candidates past the window are unknown. Always present so the envelope shape is stable.",
                      "items": {"type": "string"}
                    }
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
`
