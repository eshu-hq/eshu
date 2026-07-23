// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsInfrastructure = `
    "/api/v0/infra/resources/search": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Search infrastructure resources",
        "description": "Searches graph-backed infrastructure resources by name, ID, resource type, cloud ARN, cloud resource ID, or bounded structured filters. Provide query or at least one structured filter.",
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
          "200": {
            "description": "Infrastructure resources",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "results": {"type": "array", "items": {"type": "object", "properties": {
                      "id": {"type": "string"},
                      "name": {"type": "string"},
                      "labels": {"type": "array", "items": {"type": "string"}},
                      "kind": {"type": "string"},
                      "provider": {"type": "string"},
                      "environment": {"type": "string"},
                      "source": {"type": "string"},
                      "config_path": {"type": "string"},
                      "resource_type": {"type": "string"},
                      "resource_service": {"type": "string"},
                      "resource_category": {"type": "string"},
                      "resource_id": {"type": "string"},
                      "arn": {"type": "string"},
                      "account_id": {"type": "string"},
                      "region": {"type": "string"},
                      "service_kind": {"type": "string"}
                    }}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/infra/relationships": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "Get infrastructure relationships",
        "description": "Returns the relationships for an infrastructure entity. relationship_type is optional: when omitted every relationship is returned in both directions; when set the read is bounded to the matching edge types. Accepts a semantic alias (what_deploys, what_provisions, who_consumes_xrd, module_consumers, what_runs_image) or a canonical edge type (e.g. DEPLOYS_FROM, USES_MODULE, RUNS_IMAGE). The what_deploys alias spans the full deployment topology (DEPLOYS_FROM, DEPLOYMENT_SOURCE, HAS_DEPLOYMENT_EVIDENCE) so runtime deployment-source edges are not dropped. An unrecognized value is rejected with 400.",
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
                    "enum": ["what_deploys", "what_provisions", "who_consumes_xrd", "module_consumers", "what_runs_image", "DEPLOYS_FROM", "DEPLOYMENT_SOURCE", "HAS_DEPLOYMENT_EVIDENCE", "PROVISIONS_DEPENDENCY_FOR", "PROVISIONS_PLATFORM", "USES_MODULE", "DEPENDS_ON", "INSTANCE_OF", "RUNS_ON", "RUNS_IMAGE", "DISCOVERS_CONFIG_IN", "READS_CONFIG_FROM", "DEFINES"]
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Entity relationships",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": {"type": "string"},
                    "name": {"type": "string"},
                    "labels": {"type": "array", "items": {"type": "string"}},
                    "outgoing": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}},
                    "incoming": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}}
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
    "/api/v0/ecosystem/overview": {
      "get": {
        "tags": ["infrastructure"],
        "summary": "Get ecosystem overview",
        "description": "Returns high-level entity counts from the graph. Scoped tokens receive counts restricted to entities reachable from the caller's granted repositories via DEFINES/INSTANCE_OF/RUNS_ON; a scoped caller with no grants receives all-zero counts without a graph read.",
        "operationId": "getEcosystemOverview",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Ecosystem overview",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_count": {"type": "integer"},
                    "workload_count": {"type": "integer"},
                    "platform_count": {"type": "integer"},
                    "instance_count": {"type": "integer"}
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
        "description": "Returns a bounded, summary-first graph packet: hot entities (most-connected functions by call degree), key relationship type counts, and a per-scope ecosystem map. With repo_id the packet is repo-scoped; without repo_id only bounded ecosystem-wide label counts plus a needs-repo note are returned. Never runs a whole-graph hot-entity scan. Scoped tokens must supply a granted repo_id (not_found otherwise); the ecosystem-wide packet's counts are restricted to the caller's granted repositories, matching getEcosystemOverview.",
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
          "200": {
            "description": "Graph summary packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "hot_entities": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "hot_entities_truncated": {"type": "boolean"},
                    "key_relationships": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "ecosystem_map": {"type": "object", "additionalProperties": true},
                    "note": {"type": "string"}
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
    "/api/v0/relationships/catalog": {
      "post": {
        "tags": ["infrastructure"],
        "summary": "List the typed-edge relationship verb catalog",
        "description": "Returns the fixed catalog of typed-edge relationship verbs across the code-to-cloud graph, each with its layer, a bounded whole-graph edge count, and an evidence/source label. Each verb is counted with its own bounded relationship-type aggregate that the graph backend answers from the relationship-type index; no source-label population scan and no unlabeled-node all-node scan is run. The count is the whole-graph edge population for the verb across all source labels; it may exceed the edge-slice count when a relationship type is written by more than one source label (the edge slice is anchored on the catalog entry's primary source label).",
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
          "200": {
            "description": "Relationship verb catalog",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "verbs": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "verb": {"type": "string"},
                          "layer": {"type": "string"},
                          "count": {"type": "integer"},
                          "evidence": {"type": "string"},
                          "detail": {"type": "string"},
                          "source_tools": {"type": "object", "additionalProperties": {"type": "integer"}}
                        }
                      }
                    },
                    "verb_count": {"type": "integer"},
                    "total_edges": {"type": "integer"},
                    "layer_count": {"type": "integer"}
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
        "description": "Returns a bounded slice of concrete typed edges for one catalog verb, each with its source and target endpoints plus evidence. The verb must be one of the catalog verbs; the query is anchored on that verb's source-node label, ordered by the indexed source-anchor property, and always carries a LIMIT, so the index-ordered scan short-circuits at the page boundary and the slice is bounded. Scoped tokens receive edges whose source endpoint is attributable to a granted repository/ingestion scope, and whose target endpoint is additionally grant-checked for verbs whose target carries its own tenant attribution (repository-to-repository and workload-family verbs); a scoped caller with no grants receives an empty page without a graph read.",
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
          "200": {
            "description": "Bounded typed-edge slice for the verb",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "verb": {"type": "string"},
                    "layer": {"type": "string"},
                    "evidence": {"type": "string"},
                    "detail": {"type": "string"},
                    "edges": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "source_id": {"type": "string"},
                          "source_name": {"type": "string"},
                          "target_id": {"type": "string"},
                          "target_name": {"type": "string"},
                          "evidence": {"type": "string"},
                          "source_tool": {"type": "string"}
                        }
                      }
                    },
                    "truncated": {"type": "boolean"},
                    "limit": {"type": "integer"}
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
