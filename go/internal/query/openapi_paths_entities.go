package query

const openAPIPathsEntities = `
    "/api/v0/entities/resolve": {
      "post": {
        "tags": ["entities"],
        "summary": "Resolve entity",
        "description": "Resolves an entity by name with optional type and repository filters.",
        "operationId": "resolveEntity",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["name"],
                "properties": {
                  "name": {"type": "string", "description": "Entity name to search for"},
                  "type": {"type": "string", "description": "Optional entity type filter"},
                  "repo_id": {"type": "string", "description": "Optional repository ID filter"}
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
                    "count": {"type": "integer"}
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
    "/api/v0/entities/{entity_id}/context": {
      "get": {
        "tags": ["entities"],
        "summary": "Get entity context",
        "description": "Returns context and relationships for a specific entity.",
        "operationId": "getEntityContext",
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
                    "relationships": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}}
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
                    "story": {"type": "string"}
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
    "/api/v0/services/{service_name}/context": {
      "get": {
        "tags": ["entities"],
        "summary": "Get service context",
        "description": "Returns context for a service by name.",
        "operationId": "getServiceContext",
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
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"}
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
                    "entrypoints": {"type": "array", "items": {"type": "object"}},
                    "network_paths": {"type": "array", "items": {"type": "object"}},
                    "observed_config_environments": {"type": "array", "items": {"type": "string"}},
                    "dependents": {"type": "array", "items": {"type": "object"}},
                    "consumer_repositories": {"type": "array", "items": {"type": "object"}},
                    "provisioning_source_chains": {"type": "array", "items": {"type": "object"}},
                    "deployment_evidence": {
                      "type": "object",
                      "description": "Deployment, CI, and environment evidence pointers. Artifacts include source_location plus resolved_id/generation_id for Postgres evidence drilldown; evidence_index groups those pointers by relationship type, artifact family, and evidence kind."
                    },
                    "documentation_overview": {"type": "object"},
                    "support_overview": {"type": "object"}
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
