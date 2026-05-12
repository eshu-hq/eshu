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
                  }
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
                    "affected_count": {"type": "integer"}
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
                  "environment": {"type": "string", "description": "Optional environment filter"}
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
                  "max_depth": {"type": "integer", "default": 8, "minimum": 1, "maximum": 20}
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
