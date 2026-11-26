// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrastructure

// Routes is the OpenAPI path fragment documenting the `/api/v0/*` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/infra/resources/search": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Search infrastructure resources",
        "description": "Searches graph-backed infrastructure resources by name, ID, resource type, cloud ARN, cloud resource ID, or bounded structured filters. Provide query or at least one structured filter. A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable; this route has no configuration 503, so every 503 it answers carries Retry-After. A graph read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.deployment_chain in its error envelope.",
        "operationId": "searchInfraResources",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "query": {"type": "string"},
                  "kind": {"type": "string"},
                  "provider": {"type": "string"},
                  "environment": {"type": "string"},
                  "resource_service": {"type": "string"},
                  "resource_category": {
                    "type": "string",
                    "enum": ["compute", "storage", "data", "networking", "messaging", "security", "monitoring", "cicd", "governance", "infrastructure"]
                  },
                  "category": {
                    "type": "string",
                    "enum": ["k8s", "terraform", "argocd", "crossplane", "helm", "cloud"]
                  },
                  "limit": {"type": "integer", "default": 50, "maximum": 200}
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
            "description": "Infrastructure resources",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "results": {"type": "array", "description": "Bounded graph-backed infrastructure resource rows, emitted by the query infra search handler from the hydrated row projection.", "items": {"type": "object", "description": "One infrastructure resource row. Owner: query infra search handler.", "properties": {
                      "id": {"type": "string", "description": "Graph resource identity emitted by the query infra search row projection."},
                      "name": {"type": "string", "description": "Resource display name emitted by the query infra search row projection."},
                      "labels": {"type": "array", "items": {"type": "string"}, "description": "Graph node labels emitted by the query infra search row projection."},
                      "kind": {"type": "string", "description": "Infrastructure node kind emitted by the query infra search row projection."},
                      "provider": {"type": "string", "description": "Collector provider derived by query.infraSearchProviderFromRow (empty for non-cloud nodes)."},
                      "environment": {"type": "string", "description": "Environment label emitted by the query infra search row projection."},
                      "source": {"type": "string", "description": "Source system label emitted by the query infra search row projection."},
                      "config_path": {"type": "string", "description": "Source config path emitted by the query infra search row projection."},
                      "resource_type": {"type": "string", "description": "Provider resource type emitted by the query infra search row projection (present only when the row carries one)."},
                      "resource_service": {"type": "string", "description": "Owning service emitted by the query infra search row projection (present only when the row carries one)."},
                      "resource_category": {"type": "string", "description": "Resource category emitted by the query infra search row projection (present only when the row carries one)."},
                      "resource_id": {"type": "string", "description": "Provider resource identity emitted by the query infra search row projection (present only when the row carries one)."},
                      "arn": {"type": "string", "description": "Provider ARN emitted by the query infra search row projection (present only when the row carries one)."},
                      "account_id": {"type": "string", "description": "Cloud account id emitted by the query infra search row projection (present only when the row carries one)."},
                      "region": {"type": "string", "description": "Cloud region emitted by the query infra search row projection (present only when the row carries one)."},
                      "service_kind": {"type": "string", "description": "Service kind emitted by the query infra search row projection (present only when the row carries one)."},
                      "running_image_ref": {"type": "string", "description": "The deployed container image reference (registry/repository, tag-qualified when reported) for an ECS running task or Lambda container-image function. Present only on those CloudResource types."},
                      "running_image_digest": {"type": "string", "description": "The BARE digest (\"sha256:<hex>\", never the full registry/repository@digest reference) of the deployed image, when the source resolved one. Same shape for ECS and Lambda; see running_image_ref for the full reference."}
                    }}},
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the query infra search handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the query infra search handler from the search request limit."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the query infra search handler from the lookahead row."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/infra/relationships": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Get infrastructure relationships",
        "description": "Returns the relationships for an infrastructure entity. relationship_type is optional: when omitted every relationship is returned in both directions; when set the read is bounded to the matching edge types. Accepts a semantic alias (what_deploys, what_provisions, who_consumes_xrd, module_consumers, what_runs_image, what_runs_lambda_image) or a canonical edge type (e.g. DEPLOYS_FROM, USES_MODULE, RUNS_IMAGE, AWS_lambda_function_uses_image). The what_deploys alias spans the full deployment topology (DEPLOYS_FROM, DEPLOYMENT_SOURCE, HAS_DEPLOYMENT_EVIDENCE) so runtime deployment-source edges are not dropped. An unrecognized value is rejected with 400. A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable; this route has no configuration 503, so every 503 it answers carries Retry-After. A graph read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.deployment_chain in its error envelope.",
        "operationId": "getInfraRelationships",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["entity_id"],
                "properties": {
                  "entity_id": {"type": "string"},
                  "relationship_type": {
                    "type": "string",
                    "description": "Optional relationship filter. Semantic alias or canonical edge type; omit for all relationships.",
                    "enum": ["what_deploys", "what_provisions", "who_consumes_xrd", "module_consumers", "what_runs_image", "what_runs_lambda_image", "DEPLOYS_FROM", "DEPLOYMENT_SOURCE", "HAS_DEPLOYMENT_EVIDENCE", "PROVISIONS_DEPENDENCY_FOR", "PROVISIONS_PLATFORM", "USES_MODULE", "DEPENDS_ON", "INSTANCE_OF", "RUNS_ON", "RUNS_IMAGE", "AWS_lambda_function_uses_image", "DISCOVERS_CONFIG_IN", "READS_CONFIG_FROM", "DEFINES"]
                  }
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "200": {
            "description": "Entity relationships",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": {"type": "string", "description": "Graph entity identity emitted by the query infra relationships read from the entity row."},
                    "name": {"type": "string", "description": "Graph entity display name emitted by the query infra relationships read from the entity row."},
                    "labels": {"type": "array", "items": {"type": "string"}, "description": "Graph entity labels emitted by the query infra relationships read from the entity row."},
                    "outgoing": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}, "description": "Outgoing typed edges filtered to the requested relationship types, emitted by the query infra relationships read (component shape)."},
                    "incoming": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}, "description": "Incoming typed edges filtered to the requested relationship types, emitted by the query infra relationships read (component shape)."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/ecosystem/overview": {
      "get": {
        "tags": ["infrastructure"],
        "summary": "Get ecosystem overview",
        "description": "Returns high-level entity counts from the graph. Scoped tokens receive counts restricted to entities reachable from the caller's granted repositories via DEFINES/INSTANCE_OF/RUNS_ON; a scoped caller with no grants receives all-zero counts without a graph read. A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable; this route has no configuration 503, so every 503 it answers carries Retry-After. A graph read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.context_overview in its error envelope.",
        "operationId": "getEcosystemOverview",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Ecosystem overview",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_count": {"type": "integer", "description": "Repository node count emitted by query.runEcosystemOverviewCounts (zero for a scoped caller with no grants, without a graph read)."},
                    "workload_count": {"type": "integer", "description": "Workload node count emitted by query.runEcosystemOverviewCounts (zero for a scoped caller with no grants, without a graph read)."},
                    "platform_count": {"type": "integer", "description": "Platform node count emitted by query.runEcosystemOverviewCounts (zero for a scoped caller with no grants, without a graph read)."},
                    "instance_count": {"type": "integer", "description": "Workload-instance node count emitted by query.runEcosystemOverviewCounts (zero for a scoped caller with no grants, without a graph read)."}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/ecosystem/graph-summary": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Get graph summary packet",
        "description": "Returns a bounded, summary-first graph packet: hot entities (most-connected functions by call degree), key relationship type counts, and a per-scope ecosystem map. With repo_id the packet is repo-scoped; without repo_id only bounded ecosystem-wide label counts plus a needs-repo note are returned. Never runs a whole-graph hot-entity scan. Scoped tokens must supply a granted repo_id (not_found otherwise); the ecosystem-wide packet's counts are restricted to the caller's granted repositories, matching getEcosystemOverview. A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable; a 503 without Retry-After means a configuration state, such as no graph backend configured on this API instance. A graph read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.graph_summary_packet in its error envelope.",
        "operationId": "getGraphSummaryPacket",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Send an empty object {} for the ecosystem-wide packet, or set repo_id for the repo-scoped packet.",
                "properties": {
                  "repo_id": {"type": "string"},
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
            "description": "Graph summary packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope": {"type": "string", "description": "Packet scope emitted by the query graph-summary handler (repository for the repo-scoped packet, ecosystem for the ecosystem-wide packet)."},
                    "repo_id": {"type": "string", "description": "Echo of the caller's repo_id, emitted by the query graph-summary handler from the repo-scoped packet request (absent on the ecosystem-wide packet)."},
                    "hot_entities": {
                      "type": "array",
                      "description": "Most-connected functions by call degree, emitted by query.graphSummaryHotEntities (Neo4j degree aggregate with the codemodel raw-path fallback).",
                      "items": {
                        "type": "object",
                        "description": "One most-connected function. Owner: query.graphSummaryHotEntities.",
                        "properties": {
                          "function_id": {"type": "string", "description": "Function identity (graph id with uid fallback) emitted by the query graph-summary hot-entity projection."},
                          "function_name": {"type": "string", "description": "Function name emitted by the query graph-summary hot-entity projection."},
                          "file_path": {"type": "string", "description": "File path of the function emitted by the query graph-summary hot-entity projection."},
                          "incoming_calls": {"type": "integer", "description": "Incoming CALLS edge count emitted by the query graph-summary hot-entity projection."},
                          "outgoing_calls": {"type": "integer", "description": "Outgoing CALLS edge count emitted by the query graph-summary hot-entity projection."},
                          "total_degree": {"type": "integer", "description": "Total call degree emitted by the query graph-summary hot-entity projection."}
                        },
                        "required": ["function_id", "function_name", "file_path", "incoming_calls", "outgoing_calls", "total_degree"]
                      }
                    },
                    "hot_entities_truncated": {"type": "boolean", "description": "True when the hot-entity list hit the degree rank limit, emitted by query.graphSummaryHotEntities."},
                    "key_relationships": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Bounded per-type edge counts emitted by query.graphSummaryRelationshipCounts. Keys are the fixed code relationship types (CALLS, IMPORTS, INHERITS, OVERRIDES, REFERENCES); values are the repo-scoped edge counts."},
                    "ecosystem_map": {"type": "object", "additionalProperties": true, "description": "Structural count map emitted by query.graphSummaryRepoEcosystemMap (repo-scoped: file_count, workload_count, platform_count, dependency_count plus the languages string list) or query.runEcosystemOverviewCounts (ecosystem-wide: repo_count, workload_count, platform_count, instance_count). Keys are the count field names; values are counts (or the languages list on the repo-scoped packet)."},
                    "note": {"type": "string", "description": "Needs-repo note emitted by the query graph-summary handler on the ecosystem-wide packet (absent on the repo-scoped packet)."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "422": {"description": "Scope too broad: the repo-scoped graph summary exceeds the internal edge scan limit"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/relationships/catalog": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "List the typed-edge relationship verb catalog",
        "description": "Returns the fixed catalog of typed-edge relationship verbs across the code-to-cloud graph, each with its layer, a bounded whole-graph edge count, and an evidence/source label. Each verb is counted with its own bounded relationship-type aggregate that the graph backend answers from the relationship-type index; no source-label population scan and no unlabeled-node all-node scan is run. The count is the whole-graph edge population for the verb across all source labels; it may exceed the edge-slice count when a relationship type is written by more than one source label (the edge slice is anchored on the catalog entry's primary source label). A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable; a 503 without Retry-After means a configuration state, such as no graph backend configured on this API instance. A graph read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.relationships_catalog in its error envelope.",
        "operationId": "getRelationshipsCatalog",
        "requestBody": {
          "required": false,
          "content": {
            "application/json": {
              "schema": {"type": "object", "description": "Send an empty object {}.", "additionalProperties": false}
            }
          }
        },
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Relationship verb catalog",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "verbs": {
                      "type": "array",
                      "description": "Typed-edge verb catalog entries, emitted by the query relationships-catalog handler as []query.relationshipVerbTile.",
                      "items": {
                        "type": "object",
                        "description": "One typed-edge verb catalog entry. Owner: query relationships-catalog handler (query.relationshipVerbTile).",
                        "properties": {
                          "verb": {"type": "string", "description": "Relationship verb emitted by query.relationshipVerbTile.Verb."},
                          "layer": {"type": "string", "description": "Graph layer of the verb emitted by query.relationshipVerbTile.Layer."},
                          "count": {"type": "integer", "description": "Whole-graph edge population for the verb emitted by query.relationshipVerbTile.Count."},
                          "evidence": {"type": "string", "description": "Evidence/source label for the verb emitted by query.relationshipVerbTile.Evidence."},
                          "detail": {"type": "string", "description": "Verb detail emitted by query.relationshipVerbTile.Detail."},
                          "source_tools": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Per-source-tool edge breakdown emitted by query.relationshipVerbTile.SourceTools (present only for Tier-2 verbs that stamp source_tool). Keys are the canonical source-tool tokens; values are the per-token edge counts."}
                        },
                        "required": ["verb", "layer", "count", "evidence", "detail"]
                      }
                    },
                    "verb_count": {"type": "integer", "description": "Catalog verbs returned, emitted by the query relationships-catalog handler as len(tiles)."},
                    "total_edges": {"type": "integer", "description": "Whole-graph edge population summed across verbs, emitted by the query relationships-catalog handler."},
                    "layer_count": {"type": "integer", "description": "Distinct layers in the catalog, emitted by the query relationships-catalog handler as len(layers)."}
                  }
                }
              }
            }
          },
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/relationships/edges": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "List concrete edges for one relationship verb",
        "description": "Returns a bounded slice of concrete typed edges for one catalog verb, each with its source and target endpoints plus evidence. The verb must be one of the catalog verbs; the query is anchored on that verb's source-node label, ordered by the indexed source-anchor property, and always carries a LIMIT, so the index-ordered scan short-circuits at the page boundary and the slice is bounded. Scoped tokens receive edges whose source endpoint is attributable to a granted repository/ingestion scope, and whose target endpoint is additionally grant-checked for verbs whose target carries its own tenant attribution (repository-to-repository and workload-family verbs); a scoped caller with no grants receives an empty page without a graph read. A 503 with Retry-After means a transient condition that is safe to retry: the graph backend is unavailable; a 503 without Retry-After means a configuration state, such as no graph backend configured on this API instance. A graph read that exceeds its deadline answers 504. No 5xx body carries backend error text. A graph or reader verdict 503 names capability platform_impact.relationships_catalog in its error envelope.",
        "operationId": "getRelationshipEdges",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["verb"],
                "properties": {
                  "verb": {"type": "string", "description": "A relationship verb from the catalog, e.g. CALLS, IMPORTS, RUNS_ON."},
                  "source_tool": {"type": "string", "description": "Filter edges to one source tool (canonical vocabulary). Only Tier-2 shared verbs (DEPLOYS_FROM, USES_MODULE, and similar) stamp source_tool, so only those relationships are filterable this way. Tier-1 self-labeling tools — e.g. atlantis — attribute by edge TYPE and never carry this stamp, so they never match this filter; query those relationships by verb instead, and use the catalog endpoint's source_tools breakdown to see which tokens actually occur for a verb. See the edge-source-tool-provenance reference for the full per-token tier table.", "enum": ["terraform", "terragrunt", "helm", "kustomize", "argocd", "flux", "ansible", "puppet", "chef", "salt", "jenkins", "github_actions", "docker", "docker_compose", "gcp", "atlantis", "gitlab", "gomod", "npm", "pip", "maven", "cargo", "aws", "azure", "kubernetes", "oci", "unknown"]},
                  "limit": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded typed-edge slice for the verb",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "verb": {"type": "string", "description": "Echo of the caller's catalog verb, emitted by the query relationship-edges handler from the resolved catalog entry."},
                    "layer": {"type": "string", "description": "Graph layer of the verb, emitted by the query relationship-edges handler from the resolved catalog entry."},
                    "evidence": {"type": "string", "description": "Evidence/source label of the verb, emitted by the query relationship-edges handler from the resolved catalog entry."},
                    "detail": {"type": "string", "description": "Verb detail, emitted by the query relationship-edges handler from the resolved catalog entry."},
                    "edges": {
                      "type": "array",
                      "description": "Bounded concrete typed edges for the verb, emitted by the query relationship-edges handler as []query.relationshipEdge.",
                      "items": {
                        "type": "object",
                        "description": "One concrete typed edge. Owner: query relationship-edges handler (query.relationshipEdge).",
                        "properties": {
                          "source_id": {"type": "string", "description": "Source endpoint identity emitted by query.relationshipEdge.SourceID."},
                          "source_name": {"type": "string", "description": "Source endpoint name emitted by query.relationshipEdge.SourceName."},
                          "target_id": {"type": "string", "description": "Canonical target endpoint identity emitted by query.relationshipEdge.TargetID. Usually the target's id or uid; for verbs whose target has neither, it is the target's own identity key. IMPORTS targets a Module identified by (name, language) and so reads name@language, e.g. time@go, with an empty suffix (time@) when the language could not be determined."},
                          "target_name": {"type": "string", "description": "Target endpoint name emitted by query.relationshipEdge.TargetName."},
                          "evidence": {"type": "string", "description": "Edge evidence label emitted by query.relationshipEdge.Evidence."},
                          "source_tool": {"type": "string", "description": "Source-tool token stamped by the Tier-2 resolver, emitted by query.relationshipEdge.SourceTool (present only when the edge carries one)."}
                        },
                        "required": ["source_id", "source_name", "target_id", "target_name"]
                      }
                    },
                    "truncated": {"type": "boolean", "description": "True when more edges exist past this page, emitted by the query relationship-edges handler from the lookahead row."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the query relationship-edges handler from the clamped edges limit."},
                    "source_tool": {"type": "string", "description": "Echo of the caller's source_tool filter, emitted by the query relationship-edges handler (present only when the caller filtered to one source tool)."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
