package query

const openAPIPathsRepositories = `
    "/health": {
      "get": {
        "tags": ["health"],
        "summary": "Health check",
        "description": "Returns the health status of the API service.",
        "operationId": "getHealth",
        "responses": {
          "200": {
            "description": "Service is healthy",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string", "example": "ok"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/repositories": {
      "get": {
        "tags": ["repositories"],
        "summary": "List repositories",
        "description": "Returns a bounded page of indexed repositories.",
        "operationId": "listRepositories",
        "parameters": [
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 100, "minimum": 1, "maximum": 500}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}}
        ],
        "responses": {
          "200": {
            "description": "List of repositories",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repositories": {
                      "type": "array",
                      "items": {"$ref": "#/components/schemas/Repository"}
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/repositories/{repo_id}/context": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository context",
        "description": "Returns repository metadata with graph statistics.",
        "operationId": "getRepositoryContext",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"}
        ],
        "responses": {
          "200": {
            "description": "Repository context",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef"},
                    "file_count": {"type": "integer"},
                    "workload_count": {"type": "integer"},
                    "platform_count": {"type": "integer"},
                    "dependency_count": {"type": "integer"},
                    "relationships": {
                      "type": "array",
                      "description": "Outgoing repository relationships.",
                      "items": {"type": "object"}
                    },
                    "relationship_overview": {
                      "type": "object",
                      "description": "Incoming and outgoing typed repository relationships with lightweight evidence pointers."
                    },
                    "api_surface": {
                      "type": "object",
                      "description": "Graph-backed API endpoint surface exposed by this repository."
                    },
                    "deployment_evidence": {
                      "type": "object",
                      "description": "Deployment, CI, and environment evidence pointers. Artifacts include source_location plus resolved_id/generation_id for Postgres evidence drilldown; evidence_index groups those pointers by relationship type, artifact family, and evidence kind."
                    },
                    "consumers": {
                      "type": "array",
                      "description": "Repositories with incoming relationships to this repository.",
                      "items": {"type": "object"}
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
    "/api/v0/repositories/{repo_id}/story": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository story",
        "description": "Returns a structured repository story with deployment and support overviews.",
        "operationId": "getRepositoryStory",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"}
        ],
        "responses": {
          "200": {
            "description": "Repository narrative",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef"},
                    "subject": {"type": "object"},
                    "story": {"type": "string"},
                    "story_sections": {"type": "array", "items": {"type": "object"}},
                    "semantic_overview": {"type": "object"},
                    "deployment_overview": {"type": "object"},
                    "gitops_overview": {"type": "object"},
                    "documentation_overview": {"type": "object"},
                    "support_overview": {"type": "object"},
                    "coverage_summary": {"type": "object"},
                    "limitations": {"type": "array", "items": {"type": "string"}},
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
    "/api/v0/repositories/{repo_id}/stats": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository statistics",
        "description": "Returns repository statistics including entity counts.",
        "operationId": "getRepositoryStats",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"}
        ],
        "responses": {
          "200": {
            "description": "Repository statistics",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef"},
                    "file_count": {"type": "integer"},
                    "languages": {"type": "array", "items": {"type": "string"}},
                    "entity_count": {"type": "integer"},
                    "entity_types": {"type": "array", "items": {"type": "string"}}
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
    "/api/v0/repositories/{repo_id}/coverage": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository coverage",
        "description": "Returns content store coverage metrics for the repository.",
        "operationId": "getRepositoryCoverage",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"}
        ],
        "responses": {
          "200": {
            "description": "Repository coverage",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_id": {"type": "string"},
                    "completeness_state": {"type": "string"},
                    "graph_available": {"type": "boolean"},
                    "server_content_available": {"type": "boolean"},
                    "graph_gap_count": {"type": "integer"},
                    "content_gap_count": {"type": "integer"},
                    "file_count": {"type": "integer"},
                    "entity_count": {"type": "integer"},
                    "content_last_indexed_at": {"type": "string"},
                    "last_error": {"type": "string"},
                    "languages": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "language": {"type": "string"},
                          "file_count": {"type": "integer"}
                        }
                      }
                    },
                    "summary": {"type": "object"}
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
