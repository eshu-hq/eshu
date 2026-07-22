// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
        "description": "Returns a bounded page of indexed repositories. This route also serves the inventory (empty-selector) form of get_repository_stats, so the response carries an additive result_limits drilldown block and an explicit partial_reasons slot alongside the existing truncated paging field.",
        "operationId": "listRepositories",
        "x-scoped-token-support": true,
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
                    "count": {"type": "integer", "description": "Number of repositories returned in this page."},
                    "total": {"type": "integer", "description": "True total repository count independent of page size. Use this field — not count — to display the overall repository count on dashboards and sidebars."},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "result_limits": {"type": "object", "description": "Additive drilldown block for the inventory form of get_repository_stats: bounded page limit/offset, deterministic ordering, repository count, truncation flag, and the get_repository_stats drilldown plus inventory context path.", "additionalProperties": true},
                    "partial_reasons": {"type": "array", "description": "Explicit reasons the inventory page is partial, e.g. repository_inventory_truncated when more repositories exist beyond the page; always present so the envelope shape is stable.", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/repositories/by-language": {
      "get": {
        "tags": ["repositories"],
        "summary": "List repositories by language",
        "description": "Returns aggregate counts and a bounded page of repositories that contain files for a language family. Language aliases such as typescript include tsx, javascript includes jsx, and terraform includes hcl and tfvars. Scoped tokens receive counts and rows intersected with the caller's granted repositories/ingestion scopes; a scoped caller with no grants receives an empty page without a query.",
        "operationId": "listRepositoriesByLanguage",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "language", "in": "query", "required": true, "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 100, "minimum": 0, "maximum": 500}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}}
        ],
        "responses": {
          "200": {
            "description": "Repository language count and page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "language": {"type": "string"},
                    "normalized_languages": {"type": "array", "items": {"type": "string"}},
                    "repository_count": {"type": "integer"},
                    "file_count": {"type": "integer"},
                    "last_indexed_at": {"type": "string", "format": "date-time"},
                    "repositories": {
                      "type": "array",
                      "items": {
                        "allOf": [
                          {"$ref": "#/components/schemas/Repository"},
                          {
                            "type": "object",
                            "properties": {
                              "file_count": {"type": "integer"},
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
                              "last_indexed_at": {"type": "string", "format": "date-time"}
                            }
                          }
                        ]
                      }
                    },
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/repositories/language-inventory": {
      "get": {
        "tags": ["repositories"],
        "summary": "List repository language inventory",
        "description": "Returns aggregate repository and file counts for indexed language buckets without fetching per-repository coverage. Scoped tokens receive counts intersected with the caller's granted repositories/ingestion scopes; a scoped caller with no grants receives an empty page without a query.",
        "operationId": "getRepositoryLanguageInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 100, "minimum": 1, "maximum": 500}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}}
        ],
        "responses": {
          "200": {
            "description": "Language inventory aggregates",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "languages": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "language": {"type": "string"},
                          "repository_count": {"type": "integer"},
                          "file_count": {"type": "integer"},
                          "last_indexed_at": {"type": "string", "format": "date-time"}
                        }
                      }
                    },
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/catalog": {
      "get": {
        "tags": ["catalog"],
        "summary": "List catalog entities",
        "description": "Returns bounded repository, workload, and service handles for catalog navigation.",
        "operationId": "listCatalog",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {"type": "integer", "default": 2000, "maximum": 5000},
            "description": "Maximum rows returned per entity collection."
          }
        ],
        "responses": {
          "200": {
            "description": "Catalog entity handles",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repositories": {
                      "type": "array",
                      "items": {"$ref": "#/components/schemas/Repository"}
                    },
                    "workloads": {
                      "type": "array",
                      "items": {"$ref": "#/components/schemas/CatalogWorkload"}
                    },
                    "services": {
                      "type": "array",
                      "items": {"$ref": "#/components/schemas/CatalogWorkload"}
                    },
                    "counts": {"type": "object"},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "workloads_truncated": {"type": "boolean", "description": "True only when the workload and service collections are a bounded partial page; repository-only truncation does not set this field."},
                    "limitations": {"type": "array", "items": {"type": "string"}}
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
        "description": "Returns repository metadata with graph statistics. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryContext",
        "x-scoped-token-support": true,
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
        "description": "Returns a structured repository story with deployment, support, and bounded content-store coverage overviews. Missing coverage remains explicit and does not trigger whole-graph traversal. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryStory",
        "x-scoped-token-support": true,
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
                    "drilldowns": {"type": "object"},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer metadata with schema_version, evidence_handles, missing_evidence, limitations, truncated, coverage, partial_reasons, and recommended_next_calls."},
                    "evidence_boundaries": ` + openAPIEvidenceBoundariesSchema + `
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
    "/api/v0/repositories/{repo_id}/tree": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository file tree",
        "description": "Lists one directory level (or the full subtree with recursive=true) reconstructed from the content-store file index. Returns directory and file entries; child_count on a directory is the number of descendant files. The ref reflects the indexed commit SHA the tree was built from. When ref is supplied, it must resolve to the indexed commit; known but unindexed refs return 409 instead of silently falling back. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryTree",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"},
          {"name": "path", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Directory subpath to list, relative to the repository root."},
          {"name": "ref", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Branch name or commit SHA selector. The request succeeds only when the selector resolves to the indexed commit; unavailable or unindexed refs return an error."},
          {"name": "recursive", "in": "query", "required": false, "schema": {"type": "boolean"}, "description": "When true, return the full subtree instead of a single directory level."},
          {"name": "language", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Filter the listing to files of this language/source-type (e.g. go, python, hcl, yaml). Aliases expand to a family: typescript also matches tsx; terraform also matches hcl/tfvars. A path with no matching files returns an empty listing, not 404."}
        ],
        "responses": {
          "200": {
            "description": "Repository file tree",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "ref": {"type": "string"},
                    "path": {"type": "string"},
                    "truncated": {"type": "boolean"},
                    "entries": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "name": {"type": "string"},
                          "type": {"type": "string", "enum": ["dir", "file"]},
                          "path": {"type": "string"},
                          "size": {"type": "integer"},
                          "language": {"type": "string"},
                          "child_count": {"type": "integer"}
                        }
                      }
                    }
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
    "/api/v0/repositories/{repo_id}/content": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository file content",
        "description": "Returns the indexed bytes of a single repository file from the content store. Text is returned as utf-8; non-UTF-8 bytes are base64-encoded. size is the original byte length and truncated=true signals the byte cap was reached. When ref is supplied, it must resolve to the indexed commit; known but unindexed refs return 409 instead of silently falling back. The endpoint never returns content the collectors redact.",
        "operationId": "getRepositoryContent",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"},
          {"name": "path", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Repository-relative file path to read."},
          {"name": "ref", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Branch name or commit SHA selector. The request succeeds only when the selector resolves to the indexed commit; unavailable or unindexed refs return an error."}
        ],
        "responses": {
          "200": {
            "description": "Repository file content",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "path": {"type": "string"},
                    "ref": {"type": "string"},
                    "encoding": {"type": "string", "enum": ["utf-8", "base64"]},
                    "content": {"type": "string"},
                    "size": {"type": "integer"},
                    "language": {"type": "string"},
                    "truncated": {"type": "boolean"}
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
    "/api/v0/repositories/{repo_id}/coverage": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository coverage",
        "description": "Returns content store coverage metrics for the repository. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryCoverage",
        "x-scoped-token-support": true,
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
