// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsImpact = `
    "/api/v0/impact/trace-deployment-chain": {
      "post": {
        "tags": ["impact"],
        "summary": "Trace deployment chain",
        "description": "Returns a story-first deployment trace for a service, including deployment overview and normalized deployment fact summary. Scoped tokens receive the same shape; a service outside the caller's grant 404s and cross-repository deployment-source evidence outside the grant is withheld.",
        "operationId": "traceDeploymentChain",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["service_name"],
                "properties": {
                  "service_name": {"type": "string", "description": "Service or workload name to trace"},
                  "direct_only": {"type": "boolean", "default": true},
                  "max_depth": {"type": "integer", "default": 8, "minimum": 1},
                  "include_related_module_usage": {"type": "boolean", "default": false}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Deployment trace",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string"},
                    "workload_id": {"type": "string"},
                    "subject": {"type": "object"},
                    "name": {"type": "string"},
                    "kind": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "repo_name": {"type": "string"},
                    "story": {"type": "string"},
                    "instances": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "instance_id": {"type": "string"},
                          "environment": {"type": "string"},
                          "platforms": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "required": ["topology_basis"],
                              "properties": {
                                "platform_id": {"type": "string", "description": "Canonical graph Platform identity; never derived from the display name."},
                                "platform_name": {"type": "string"},
                                "platform_kind": {"type": "string"},
                                "platform_confidence": {"type": "number"},
                                "platform_reason": {"type": "string"},
                                "topology_basis": {"type": "string", "enum": ["direct_runtime"], "description": "The platform is supported by an exact WorkloadInstance RUNS_ON Platform relationship."},
                                "topology_edges": {
                                  "type": "array",
                                  "description": "Exact canonical RUNS_ON graph relationships supporting this runtime platform.",
                                  "items": {
                                    "type": "object",
                                    "required": ["relationship_type", "source_id", "target_id"],
                                    "properties": {
                                      "relationship_type": {"type": "string", "enum": ["RUNS_ON"]},
                                      "source_id": {"type": "string"},
                                      "source_name": {"type": "string"},
                                      "target_id": {"type": "string"},
                                      "target_name": {"type": "string"},
                                      "confidence": {"type": "number"},
                                      "reason": {"type": "string"},
                                      "evidence_source": {"type": "string"},
                                      "source_tool": {"type": "string"},
                                      "properties": {"type": "object", "additionalProperties": true, "description": "Exact observed relationship properties retained as evidence provenance."}
                                    }
                                  }
                                }
                              }
                            }
                          }
                        }
                      }
                    },
                    "topology_edges": {
                      "type": "array",
                      "description": "Exact graph-observed repository/workload and instance/workload identity edges. Endpoints are never inferred from ids.",
                      "items": {
                        "type": "object",
                        "required": ["relationship_type", "source_id", "target_id", "properties"],
                        "properties": {
                          "relationship_type": {"type": "string", "enum": ["DEFINES", "INSTANCE_OF"]},
                          "source_id": {"type": "string"},
                          "source_name": {"type": "string"},
                          "target_id": {"type": "string"},
                          "target_name": {"type": "string"},
                          "confidence": {"type": "number"},
                          "reason": {"type": "string"},
                          "evidence_source": {"type": "string"},
                          "source_tool": {"type": "string"},
                          "properties": {"type": "object", "additionalProperties": true}
                        }
                      }
                    },
                    "provisioned_platforms": {
                      "type": "array",
                      "description": "Repository-level provisioning evidence kept separate from runtime instance placement. These rows never imply RUNS_ON.",
                      "items": {
                        "type": "object",
                        "required": ["topology_basis"],
                        "properties": {
                          "platform_id": {"type": "string"},
                          "platform_name": {"type": "string"},
                          "platform_kind": {"type": "string"},
                          "platform_provider": {"type": "string"},
                          "platform_region": {"type": "string"},
                          "platform_locator": {"type": "string"},
                          "platform_confidence": {"type": "number"},
                          "platform_reason": {"type": "string"},
                          "topology_basis": {"type": "string", "enum": ["provisioning_fallback"], "description": "The platform is supported by exact repository provisioning relationships, not a WorkloadInstance RUNS_ON relationship."},
                          "topology_edges": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "required": ["relationship_type", "source_id", "target_id", "properties"],
                              "properties": {
                                "relationship_type": {"type": "string", "enum": ["PROVISIONS_DEPENDENCY_FOR", "PROVISIONS_PLATFORM"]},
                                "source_id": {"type": "string"},
                                "source_name": {"type": "string"},
                                "target_id": {"type": "string"},
                                "target_name": {"type": "string"},
                                "confidence": {"type": "number"},
                                "reason": {"type": "string"},
                                "evidence_source": {"type": "string"},
                                "source_tool": {"type": "string"},
                                "properties": {"type": "object", "additionalProperties": true}
                              }
                            }
                          }
                        }
                      }
                    },
                    "runtime_topology_limits": ` + openAPIImpactRuntimeTopologyLimits + `,
                    "hostnames": {"type": "array", "items": {"type": "object"}},
                    "entrypoints": {"type": "array", "items": {"type": "object"}},
                    "network_paths": {"type": "array", "items": {"type": "object"}},
                    "observed_config_environments": {"type": "array", "items": {"type": "string"}},
                    "api_surface": {"type": "object"},
                    "dependents": {"type": "array", "items": {"type": "object"}},
                    "deployment_sources": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "repo_id": {"type": "string"},
                          "repo_name": {"type": "string"},
                          "relationship_type": {"type": "string", "enum": ["DEPLOYMENT_SOURCE", "DEPLOYS_FROM"]},
                          "source_id": {"type": "string", "description": "Canonical source endpoint identity for relationship_type."},
                          "target_id": {"type": "string", "description": "Canonical target endpoint identity for relationship_type."},
                          "confidence": {"type": "number"},
                          "reason": {"type": "string"}
                        }
                      }
                    },
                    "deployment_source_limits": {
                      "type": "object",
                      "description": "Coverage and deterministic bound metadata for deployment_sources. When observed_count_is_lower_bound is true, observed_count is the number seen through the per-query sentinel rather than an exact total.",
                      "required": ["limit", "query_sentinel_limit", "returned_count", "observed_count", "observed_count_is_lower_bound", "canonical_observed_count", "repository_observed_count", "truncated", "ordering"],
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
                    "cloud_resources": {"type": "array", "description": "CloudResource dependencies admitted only from materialized WorkloadInstance USES CloudResource relationships. Repository-scoped callers receive none until those global relationships carry repository ownership.", "items": {"type": "object"}},
                    "cloud_resource_limits": {
                      "type": "object",
                      "description": "Deterministic resource and relationship-observation bounds for cloud_resources. observation_count_is_lower_bound is true when either the bounded pre-aggregation row probe or the resource-row probe reached its sentinel. Observation-only truncation does not prove that a whole resource identity was omitted.",
                      "required": ["limit", "query_sentinel_limit", "returned_count", "observed_count", "observed_count_is_lower_bound", "observation_limit", "observation_query_sentinel_limit", "observation_count", "observation_count_is_lower_bound", "truncated", "ordering"],
                      "properties": {
                        "limit": {"type": "integer"},
                        "query_sentinel_limit": {"type": "integer"},
                        "returned_count": {"type": "integer"},
                        "observed_count": {"type": "integer"},
                        "observed_count_is_lower_bound": {"type": "boolean"},
                        "observation_limit": {"type": "integer"},
                        "observation_query_sentinel_limit": {"type": "integer"},
                        "observation_count": {"type": "integer"},
                        "observation_count_is_lower_bound": {"type": "boolean"},
                        "truncated": {"type": "boolean"},
                        "ordering": {"type": "array", "items": {"type": "string"}}
                      }
                    },
                    "uncorrelated_cloud_resources": {"type": "array", "description": "CloudResource candidates from bounded free-text or deployment-config evidence that do not have a materialized workload-to-cloud relationship. All candidates expose candidate_status and missing_relationship. Deployment-config candidates are globally ordered by name and canonical ID before the response bound is applied. Deployment-config candidates expose match_basis; free-text candidate_status can be uncorrelated, ambiguous_anchor, stale_anchor, or weak_anchor.", "items": {"type": "object"}},
                    "uncorrelated_cloud_resources_truncated": {"type": "boolean", "description": "Present and true when candidate discovery was incomplete because the returned list was capped or deployment-config evidence or anchor input was truncated; additional candidates may exist even when no rows were returned."},
                    "k8s_resources": {"type": "array", "items": {"type": "object"}},
` + openAPIImpactK8sResourceLimits + `
                    "image_refs": {"type": "array", "items": {"type": "string"}},
                    "image_registry_truth": {"type": "array", "items": {"type": "object"}},
                    "k8s_relationships": {"type": "array", "items": {"type": "object"}},
                    "deployment_facts": {"type": "array", "items": {"type": "object"}},
                    "controller_driven_paths": {"type": "array", "items": {"type": "object"}},
                    "delivery_paths": {"type": "array", "items": {"type": "object"}},
                    "story_sections": {"type": "array", "items": {"type": "object"}},
                    "deployment_overview": {"type": "object"},
                    "gitops_overview": {"type": "object"},
                    "consumer_repositories": {"type": "array", "items": {"type": "object"}},
                    "provisioning_source_chains": {"type": "array", "items": {"type": "object"}},
                    "deployment_evidence": {"type": "object"},
                    "documentation_overview": {"type": "object"},
                    "support_overview": {"type": "object"},
                    "controller_overview": {
                      "type": "object",
                      "properties": {
                        "controller_count": {"type": "integer"},
                        "controllers": {"type": "array", "items": {"type": "string"}},
                        "controller_kinds": {"type": "array", "items": {"type": "string"}},
                        "entities": {"type": "array", "items": {"type": "object"}},
                        "entity_limits": {
                          "type": "object",
                          "description": "Bound and source-scan completeness metadata for controller entities admitted to this service trace.",
                          "properties": {
                            "limit": {"type": "integer"},
                            "source_query_sentinel_limit": {"type": "integer"},
                            "returned_count": {"type": "integer"},
                            "observed_count": {"type": "integer"},
                            "observed_count_is_lower_bound": {"type": "boolean"},
                            "truncated": {"type": "boolean"},
                            "ordering": {"type": "array", "items": {"type": "string"}}
                          }
                        }
                      }
                    },
                    "runtime_overview": {"type": "object"},
                    "deployment_fact_summary": {"type": "object"},
                    "drilldowns": {"type": "object"},
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
` + openAPIPathDeploymentConfigInfluence + `
    "/api/v0/impact/blast-radius": {
      "post": {
        "tags": ["impact"],
        "summary": "Find blast radius",
        "description": "Analyzes the blast radius for a target entity. Scoped tokens receive the same shape; affected repositories outside the caller's grant are withheld.",
        "operationId": "findBlastRadius",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["target", "target_type"],
                "properties": {
                  "target": {"type": "string", "description": "Target entity name"},
                  "target_type": {
                    "type": "string",
                    "enum": ["repository", "terraform_module", "crossplane_xrd", "sql_table"],
                    "description": "Type of target entity"
                  },
                  "limit": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Blast radius analysis",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "target": {"type": "string"},
                    "target_type": {"type": "string"},
                    "affected": {"type": "array", "items": {"type": "object"}},
                    "affected_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "complete": {
                      "type": "boolean",
                      "description": "False when target_type is sql_table or crossplane_xrd and any edge type the surface conceptually covers (see coverage) has no graph writer, so affected_count may undercount. Always false for crossplane_xrd today (SATISFIED_BY has no writer). Always true for target_type values with no registered coverage gap (repository, terraform_module)."
                    },
                    "coverage": {
                      "type": "array",
                      "description": "Materialization status of every graph edge type the target_type's blast-radius surface conceptually covers. Empty for target_type values with no registered coverage gap.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "edge_type": {"type": "string", "description": "Graph relationship type, e.g. INDEXES"},
                          "materialized": {"type": "boolean", "description": "Whether a graph writer currently emits this edge type"},
                          "reason": {"type": "string", "description": "Why the edge type is or is not materialized"}
                        }
                      }
                    }
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
    "/api/v0/impact/change-surface": {
      "post": {
        "tags": ["impact"],
        "summary": "Find change surface",
        "description": "Analyzes the change surface for a target entity. The start node is resolved through label-anchored indexed lookups (optionally hinted by kind) and the impact traversal is bounded by max_depth, so dense service-kind targets stay within the repo-scale traversal budget. Results beyond limit set truncated. Scoped tokens receive the same shape; a resolved target and every impacted row outside the caller's grant are withheld.",
        "operationId": "findChangeSurface",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["target"],
                "properties": {
                  "target": {"type": "string", "description": "Target entity ID"},
                  "kind": {"type": "string", "description": "Optional target kind hint for label-anchored resolution", "enum": ["service", "workload", "workload_instance", "repository", "resource", "cloud_resource", "terraform_module", "module"]},
                  "target_type": {"type": "string", "description": "Alias for kind (legacy field name)", "enum": ["service", "workload", "workload_instance", "repository", "resource", "cloud_resource", "terraform_module", "module"]},
                  "environment": {"type": "string", "description": "Optional environment filter"},
                  "max_depth": {"type": "integer", "description": "Maximum traversal hops (defaults to 4, clamped to 8)", "default": 4, "minimum": 1, "maximum": 8},
                  "limit": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Change surface analysis",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "target": {"type": "object"},
                    "impacted": {"type": "array", "items": {"type": "object"}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "environment": {"type": "string"}
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
    "/api/v0/impact/change-surface/investigate": {
      "post": {
        "tags": ["impact"],
        "summary": "Investigate change surface",
        "description": "Resolves a service, workload, resource, module, code topic, or changed path set into one bounded change-surface response with ambiguity metadata, code handles, direct impact, transitive impact, limits, and truncation. Scoped tokens receive the same shape; a resolved target, an explicit repo_id, and every impacted row outside the caller's grant are withheld.",
        "operationId": "investigateChangeSurface",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Provide at least one target family: target, service_name, workload_id, resource_id, module_id, topic or query, or changed_paths with repo_id.",
                "anyOf": [
                  {"required": ["target"]},
                  {"required": ["service_name"]},
                  {"required": ["workload_id"]},
                  {"required": ["resource_id"]},
                  {"required": ["module_id"]},
                  {"required": ["topic"]},
                  {"required": ["query"]},
                  {"required": ["changed_paths", "repo_id"]}
                ],
                "properties": {
                  "target": {"type": "string", "description": "Canonical entity id or exact entity name"},
                  "target_type": {"type": "string", "enum": ["service", "workload", "workload_instance", "repository", "resource", "cloud_resource", "terraform_module", "module"]},
                  "service_name": {"type": "string"},
                  "workload_id": {"type": "string"},
                  "resource_id": {"type": "string"},
                  "module_id": {"type": "string"},
                  "repo_id": {"type": "string"},
                  "topic": {"type": "string"},
                  "query": {"type": "string"},
                  "changed_paths": {"type": "array", "items": {"type": "string"}},
                  "environment": {"type": "string"},
                  "max_depth": {"type": "integer", "default": 4, "minimum": 1, "maximum": 8},
                  "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 100},
                  "offset": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Change-surface investigation",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope": {"type": "object"},
                    "target_resolution": {"type": "object"},
                    "code_surface": {"type": "object"},
                    "direct_impact": {"type": "array", "items": {"type": "object"}},
                    "transitive_impact": {"type": "array", "items": {"type": "object"}},
                    "impact_summary": {"type": "object"},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "coverage": {"type": "object"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "source_backend": {"type": "string"},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer metadata with schema_version, evidence_handles, missing_evidence, limitations, truncated, coverage, partial_reasons, and recommended_next_calls."}
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
