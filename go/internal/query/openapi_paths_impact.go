package query

const openAPIPathsImpact = `
    "/api/v0/impact/trace-deployment-chain": {
      "post": {
        "tags": ["impact"],
        "summary": "Trace deployment chain",
        "description": "Returns a story-first deployment trace for a service, including deployment overview and normalized deployment fact summary.",
        "operationId": "traceDeploymentChain",
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
                    "instances": {"type": "array", "items": {"type": "object"}},
                    "hostnames": {"type": "array", "items": {"type": "object"}},
                    "entrypoints": {"type": "array", "items": {"type": "object"}},
                    "network_paths": {"type": "array", "items": {"type": "object"}},
                    "observed_config_environments": {"type": "array", "items": {"type": "string"}},
                    "api_surface": {"type": "object"},
                    "dependents": {"type": "array", "items": {"type": "object"}},
                    "deployment_sources": {"type": "array", "items": {"type": "object"}},
                    "cloud_resources": {"type": "array", "items": {"type": "object"}},
                    "k8s_resources": {"type": "array", "items": {"type": "object"}},
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
                        "entities": {"type": "array", "items": {"type": "object"}}
                      }
                    },
                    "runtime_overview": {"type": "object"},
                    "deployment_fact_summary": {"type": "object"},
                    "drilldowns": {"type": "object"}
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
    "/api/v0/impact/deployment-config-influence": {
      "post": {
        "tags": ["impact"],
        "summary": "Investigate deployment configuration influence",
        "description": "Returns a bounded service deployment configuration story with influencing repositories, values layers, image tag sources, runtime setting sources, resource limit sources, rendered targets, and portable file handles.",
        "operationId": "investigateDeploymentConfigInfluence",
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
                    "coverage": {"type": "object"}
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
    "/api/v0/impact/blast-radius": {
      "post": {
        "tags": ["impact"],
        "summary": "Find blast radius",
        "description": "Analyzes the blast radius for a target entity.",
        "operationId": "findBlastRadius",
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
    "/api/v0/impact/change-surface": {
      "post": {
        "tags": ["impact"],
        "summary": "Find change surface",
        "description": "Analyzes the change surface for a target entity.",
        "operationId": "findChangeSurface",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["target"],
                "properties": {
                  "target": {"type": "string", "description": "Target entity ID"},
                  "environment": {"type": "string", "description": "Optional environment filter"},
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
        "description": "Resolves a service, workload, resource, module, code topic, or changed path set into one bounded change-surface response with ambiguity metadata, code handles, direct impact, transitive impact, limits, and truncation.",
        "operationId": "investigateChangeSurface",
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
                    "source_backend": {"type": "string"}
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
    "/api/v0/impact/resource-investigation": {
      "post": {
        "tags": ["impact"],
        "summary": "Investigate resource",
        "description": "Resolves a queue, database, cloud resource, Terraform resource, or Kubernetes object into a bounded investigation packet with ambiguity metadata, workload users, repository provenance paths, source handles, limitations, and recommended next calls.",
        "operationId": "investigateResource",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Provide query or resource_id.",
                "anyOf": [
                  {"required": ["query"]},
                  {"required": ["resource_id"]}
                ],
                "properties": {
                  "query": {"type": "string", "description": "Resource name, kind, queue, database, or cloud identifier to resolve"},
                  "resource_id": {"type": "string", "description": "Canonical graph resource id when already known"},
                  "resource_type": {"type": "string", "enum": ["queue", "database", "cloud_resource", "k8s_resource", "terraform_resource", "terraform_module"]},
                  "environment": {"type": "string"},
                  "max_depth": {"type": "integer", "default": 4, "minimum": 1, "maximum": 8},
                  "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Resource investigation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope": {"type": "object"},
                    "target_resolution": {"type": "object"},
                    "resource": {"type": "object"},
                    "story": {"type": "string"},
                    "workloads": {"type": "array", "items": {"type": "object"}},
                    "workload_count": {"type": "integer"},
                    "provisioning_paths": {"type": "array", "items": {"type": "object"}},
                    "source_handles": {"type": "array", "items": {"type": "object"}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "coverage": {"type": "object"},
                    "limit": {"type": "integer"},
                    "max_depth": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "environment": {"type": "string"},
                    "source_backend": {"type": "string"}
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
    "/api/v0/impact/trace-resource-to-code": {
      "post": {
        "tags": ["impact"],
        "summary": "Trace resource to code",
        "description": "Traces a resource back to its source code repositories.",
        "operationId": "traceResourceToCode",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["start"],
                "properties": {
                  "start": {"type": "string", "description": "Starting entity ID"},
                  "environment": {"type": "string"},
                  "max_depth": {"type": "integer", "default": 8, "minimum": 1, "maximum": 20},
                  "limit": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Trace paths",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "start": {"type": "object"},
                    "paths": {"type": "array", "items": {"type": "object"}},
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
    "/api/v0/impact/explain-dependency-path": {
      "post": {
        "tags": ["impact"],
        "summary": "Explain dependency path",
        "description": "Finds and explains the shortest path between two entities.",
        "operationId": "explainDependencyPath",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["source", "target"],
                "properties": {
                  "source": {"type": "string", "description": "Source entity ID"},
                  "target": {"type": "string", "description": "Target entity ID"},
                  "environment": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Dependency path",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "source": {"type": "object"},
                    "target": {"type": "object"},
                    "path": {"type": "object"},
                    "confidence": {"type": "number"},
                    "reason": {"type": "string"},
                    "environment": {"type": "string"}
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
