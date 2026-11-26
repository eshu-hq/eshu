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
        "description": "Resolves an entity by exact case-sensitive name. Omitting type with no repo_id searches every kind by exact name (repository, workload, package, and content entity), returning one ranked list where each match carries its kind, canonical id, and next selector; names under 3 characters are refused. A supplied type narrows the lookup to that index; repository, directory, and file types require repo_id. Canonical content-entity IDs and workload resolution keep their dedicated exact paths.",
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
                  "type": {"type": "string", "description": "Entity type filter. Optional: omitting it with no repo_id searches every kind by exact name. Unknown types fail closed."},
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
                      "description": "Ranked matches carrying kind, canonical id, and next selector. Owner: entity.resolvedEntityResponse via Handler.ResolveEntity.",
                      "items": {"$ref": "#/components/schemas/EntityRef"}
                    },
                    "count": {"type": "integer", "description": "Rows in this page after the limit is applied. Owner: entity.resolvedEntityResponse."},
                    "limit": {"type": "integer", "description": "Normalized page bound the lookup ran under. Owner: entity.NormalizeResolveEntityLimit via Handler.ResolveEntity."},
                    "truncated": {"type": "boolean", "description": "True when more matches exist past the page bound. Owner: entity.trimResolvedEntityPage via Handler.ResolveEntity."}
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
                    "id": {"type": "string", "description": "Canonical entity id the context was read for. Owner: entity.Handler.GetEntityContext."},
                    "labels": {"type": "array", "items": {"type": "string"}, "description": "Graph labels of the resolved entity. Owner: entity.Handler.GetEntityContext."},
                    "name": {"type": "string", "description": "Entity name from the graph row. Owner: entity.Handler.GetEntityContext."},
                    "file_path": {"type": "string", "description": "Repository-relative file path of the entity, empty when the node carries none. Owner: entity.Handler.GetEntityContext."},
                    "language": {"type": "string", "description": "Entity language, coalesced from the entity and file rows. Owner: entity.Handler.GetEntityContext."},
                    "start_line": {"type": "integer", "description": "First entity line from the graph row. Owner: entity.Handler.GetEntityContext."},
                    "end_line": {"type": "integer", "description": "Last entity line from the graph row. Owner: entity.Handler.GetEntityContext."},
                    "metadata": {"type": "object", "additionalProperties": true, "description": "Parser-emitted entity metadata keyed by parser field name, hydrated from the content store when the graph row is sparse. Owner: entity.Handler.EnrichEntityResultsWithContentMetadata."},
                    "semantic_summary": {"type": "string", "description": "One-line derived summary, present when metadata yields one. Owner: entitysemantics.AttachSemanticSummary."},
                    "semantic_profile": {"type": "object", "additionalProperties": true, "description": "Highest-signal structured semantics keyed by signal name, present when metadata yields any. Owner: entitysemantics.BuildEntitySemanticProfile via AttachSemanticSummary."},
                    "python_semantics": {"type": "object", "additionalProperties": true, "description": "Python-only semantic fields keyed by field name, present for python entities. Owner: entitysemantics.AttachSemanticSummary."},
                    "javascript_semantics": {"type": "object", "additionalProperties": true, "description": "JavaScript semantic fields keyed by field name, present when extraction yields any. Owner: entitysemantics.AttachSemanticSummary."},
                    "typescript_semantics": {"type": "object", "additionalProperties": true, "description": "TypeScript semantic fields keyed by field name, present for typescript/tsx entities. Owner: entitysemantics.AttachSemanticSummary."},
                    "story": {"type": "string", "description": "One-line entity story rendered from the semantic summary, present when a summary exists. Owner: entitysemantics.buildEntityStory via AttachSemanticSummary."},
                    "repo_id": {"type": "string", "description": "Repository id resolved through the REPO_CONTAINS file enrichment. Owner: entity.Handler.GetEntityContext."},
                    "repo_name": {"type": "string", "description": "Repository name resolved through the REPO_CONTAINS file enrichment. Owner: entity.Handler.GetEntityContext."},
                    "relationships": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}, "description": "Distinct outgoing relationships of the entity. Owner: entity.extractRelationships via GetEntityContext."},
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
                    "workload_id": {"type": "string", "description": "Resolved workload node id the story was built for. Owner: entity.Handler.GetWorkloadStory."},
                    "name": {"type": "string", "description": "Workload name from the resolved context. Owner: entity.Handler.GetWorkloadStory."},
                    "story": {"type": "string", "description": "Narrative summary rendered from the workload context. Owner: entity.buildWorkloadStory via GetWorkloadStory."},
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
          {"name": "downstream_read_limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "maximum": 100}, "description": "Indirect-evidence row bound for the dependents and consumer_repositories reads. Absent means the default 25; a value above 100 clamps to 100, and the effective bound is echoed on result_limits.downstream_read_limit and the investigation coverage. Non-numeric or negative values return 400 invalid_argument."},
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
    "/api/v0/services/{service_name}/api-surface/endpoints": {
      "get": {
        "tags": ["entities"],
        "summary": "List service API surface endpoints",
        "description": "Returns one page of a service's API surface endpoints in path, endpoint_id order. The selector matches get_service_story (service_id, workload_id, or service_name, plus repo and environment; an ambiguous selector answers 409). The story and context dossiers keep only a 50-row api_surface preview and point here; repeat with offset set to next_offset until next_offset is absent.",
        "operationId": "listServiceAPIEndpoints",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {"name": "service_id", "in": "query", "description": "Exact workload or service id selector. When supplied, the route resolves this id before service-name candidates.", "schema": {"type": "string"}},
          {"name": "repo", "in": "query", "description": "Repository selector that separates only services in different repositories; two same-named services in one repository stay ambiguous and need service_id. Accepts canonical repository id, repository name, slug, path, or remote URL.", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "description": "Runtime environment selector used to disambiguate duplicate service names by workload instance environment.", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 50}, "description": "Endpoints per page. Defaults to 50 when absent and clamps to 100; non-positive or non-numeric values return 400 invalid_argument."},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Rows to skip, in path, endpoint_id order. Defaults to 0; repeat with offset set to the previous page's next_offset until next_offset is absent."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "One page of service API surface endpoints",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string", "description": "Canonical service name the page was read for. Owner: entity.Handler.GetServiceAPISurfaceEndpoints."},
                    "truth_basis": {"type": "string", "description": "Truth basis of the surface rows, always graph. Owner: repository.buildGraphAPISurface."},
                    "endpoint_count": {"type": "integer", "description": "True total from the count query, not the page size."},
                    "method_count": {"type": "integer", "description": "HTTP methods counted across the page rows. Owner: repository.buildGraphAPISurface."},
                    "operation_id_count": {"type": "integer", "description": "Operation ids counted across the page rows. Owner: repository.buildGraphAPISurface."},
                    "endpoints": {
                      "type": "array",
                      "description": "API endpoint rows in path, endpoint_id order. Owner: repository.buildGraphAPISurface.",
                      "items": {
                        "type": "object",
                        "description": "One service API surface endpoint row. Owner: entity.Handler.GetServiceAPISurfaceEndpoints via repository.buildGraphAPISurface.",
                        "properties": {
                          "id": {"type": "string", "description": "Endpoint id from the graph row. Owner: repository.buildGraphAPISurface."},
                          "path": {"type": "string", "description": "Endpoint path from the graph row. Owner: repository.buildGraphAPISurface."},
                          "methods": {"type": "array", "items": {"type": "string"}, "description": "Lowercased HTTP methods of the endpoint. Owner: repository.buildGraphAPISurface."},
                          "operation_ids": {"type": "array", "items": {"type": "string"}, "description": "Sorted operation ids of the endpoint. Owner: repository.buildGraphAPISurface."},
                          "source": {"type": "string", "description": "Row source, always graph. Owner: repository.buildGraphAPISurface."},
                          "source_kinds": {"type": "array", "items": {"type": "string"}, "description": "Sorted parser source kinds behind the endpoint. Owner: repository.buildGraphAPISurface."},
                          "source_paths": {"type": "array", "items": {"type": "string"}, "description": "Sorted source paths behind the endpoint. Owner: repository.buildGraphAPISurface."},
                          "evidence_source": {"type": "string", "description": "Evidence source of the endpoint row. Owner: repository.buildGraphAPISurface."},
                          "workload_id": {"type": "string", "description": "Owning workload id, present when the row carries one. Owner: repository.buildGraphAPISurface."},
                          "workload_name": {"type": "string", "description": "Owning workload name, present when the row carries one. Owner: repository.buildGraphAPISurface."}
                        }
                      }
                    },
                    "source_paths": {"type": "array", "items": {"type": "string"}, "description": "Sorted source paths across the page rows. Owner: repository.buildGraphAPISurface."},
                    "spec_paths": {"type": "array", "items": {"type": "string"}, "description": "Sorted spec paths across the page rows. Owner: repository.buildGraphAPISurface."},
                    "spec_versions": {"type": "array", "items": {"type": "string"}, "description": "Sorted spec versions across the page rows. Owner: repository.buildGraphAPISurface."},
                    "api_versions": {"type": "array", "items": {"type": "string"}, "description": "Sorted API versions across the page rows. Owner: repository.buildGraphAPISurface."},
                    "detail_limit": {"type": "integer", "description": "Page size the detail read ran under."},
                    "detail_truncated": {"type": "boolean", "description": "True when rows remain past this page."},
                    "offset": {"type": "integer", "description": "Rows skipped, from the request offset. Owner: repository.QueryRepoAPISurfacePaged."},
                    "next_offset": {"type": "integer", "description": "Absent when the page is complete; pass as offset for the next page.", "nullable": true},
                    "ordering": {"type": "string", "description": "Deterministic page order: path, endpoint_id."},
                    "frameworks": {"type": "array", "items": {"type": "string"}, "description": "Parser framework names derived from source kinds, present when any row carries one. Owner: repository.buildGraphAPISurface via frameworkNamesFromSourceKinds."},
                    "framework_route_count": {"type": "integer", "description": "Endpoint rows carrying a framework source kind, present with frameworks. Owner: repository.buildGraphAPISurface via countFrameworkEndpointRows."}
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
    "/api/v0/services/{service_name}/downstream-consumers": {
      "get": {
        "tags": ["entities"],
        "summary": "List service downstream consumers",
        "description": "Returns one page of a service's downstream consumer repositories in (repository, repo_id) order. The selector matches get_service_story (service_id, workload_id, or service_name, plus repo and environment; an ambiguous selector answers 409). The story and context dossier cap downstream lists at the effective read bound; page the full set here with after_repository/after_repo_id set to the previous page's next_cursor until next_cursor is absent. lower_bound is true only when a read bound fired (then the paged set is not a census), false when the read completed without hitting a bound; use next_cursor presence, not a total, to decide whether to continue.",
        "operationId": "listServiceDownstreamConsumers",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {"name": "service_id", "in": "query", "description": "Exact workload or service identifier. When supplied, the route resolves this id before service-name candidates.", "schema": {"type": "string"}},
          {"name": "repo", "in": "query", "description": "Repository selector that separates only services in different repositories; two same-named services in one repository stay ambiguous and need service_id. Accepts canonical repository id, repository name, slug, path, or remote URL.", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "description": "Runtime environment selector used to disambiguate duplicate service names by workload instance environment.", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 50}, "description": "Consumers per page. Defaults to 50 when absent and clamps to 100; non-positive or non-numeric values return 400 invalid_argument."},
          {"name": "after_repository", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Cursor: repository of the last row of the previous page. Must be provided together with after_repo_id; absent starts at the first row."},
          {"name": "after_repo_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Cursor: repo_id of the last row of the previous page. Must be provided together with after_repository."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "One page of service downstream consumers",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string", "description": "Canonical service name the page was read for. Owner: entity.Handler.GetServiceDownstreamConsumers."},
                    "consumers": {
                      "type": "array",
                      "description": "Downstream consumer repository rows in (repository, repo_id) order. Owner: entity.Handler.GetServiceDownstreamConsumers over service.ListServiceDownstreamConsumers.",
                      "items": {"type": "object", "additionalProperties": true, "description": "One downstream consumer repository row keyed by enrichment field name: repository, repo_id, and consumer_kinds always ride; graph_relationship_types, graph_relationship_reasons, evidence_kinds, matched_values, and sample_paths ride only when that enrichment source fired. Owner: service.ConsumerListPage.Consumers via entity.Handler.GetServiceDownstreamConsumers."}
                    },
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page or a read bound fired."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Absent when the page is terminal; pass after_repository/after_repo_id from it for the next page.",
                      "nullable": true,
                      "properties": {
                        "repository": {"type": "string", "description": "Cursor repository of the last served row. Owner: entity.Handler.GetServiceDownstreamConsumers."},
                        "repo_id": {"type": "string", "description": "Cursor repo_id of the last served row. Owner: entity.Handler.GetServiceDownstreamConsumers."}
                      }
                    },
                    "consumer_truncation_sources": {
                      "type": "object",
                      "description": "Which read bound fired: candidates, evidence_files, hostnames, searches, merge_cap. Owner: entity.Handler.GetServiceDownstreamConsumers from the consumer page sources.",
                      "properties": {
                        "candidates": {"type": "boolean", "description": "True when the candidate read hit its bound. Owner: entity.Handler.GetServiceDownstreamConsumers."},
                        "evidence_files": {"type": "boolean", "description": "True when the evidence-file read hit its bound. Owner: entity.Handler.GetServiceDownstreamConsumers."},
                        "hostnames": {"type": "boolean", "description": "True when the hostname read hit its bound. Owner: entity.Handler.GetServiceDownstreamConsumers."},
                        "searches": {"type": "boolean", "description": "True when the search read hit its bound. Owner: entity.Handler.GetServiceDownstreamConsumers."},
                        "merge_cap": {"type": "boolean", "description": "True when the merged set was trimmed at the internal read ceiling. Owner: entity.Handler.GetServiceDownstreamConsumers."}
                      }
                    },
                    "searched_hostnames": {"type": "array", "items": {"type": "string"}, "description": "Hostname list actually searched."},
                    "effective_read_bound": {"type": "integer", "description": "Internal read bound the page was recomputed under."},
                    "lower_bound": {"type": "boolean", "description": "True only when a read bound fired (then the paged set is not a census); false when the read completed without hitting a bound."},
                    "ordering": {"type": "string", "description": "Deterministic page order: repository, repo_id."},
                    "page_size": {"type": "integer", "description": "Consumers per page from the request limit. Owner: entity.Handler.GetServiceDownstreamConsumers."},
                    "ceiling_reached": {"type": "boolean", "description": "Present and true only when the merged set was trimmed at the internal read ceiling."}
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
