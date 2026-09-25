// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Routes is the OpenAPI path fragment documenting the 10 `/api/v0/*` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/status/pipeline": {
      "get": {
        "tags": ["status"],
        "summary": "Get pipeline status",
        "description": "Returns the full pipeline status report.",
        "operationId": "getPipelineStatus",
        "responses": {
          "200": {
            "description": "Pipeline status",
            "content": {
              "application/json": {
                "schema": {"type": "object"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/status/ingesters": {
      "get": {
        "tags": ["status"],
        "summary": "List ingesters",
        "description": "Returns known ingesters with basic health info.",
        "operationId": "listIngesters",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "List of ingesters",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "ingesters": {"type": "array", "items": {"type": "object"}},
                    "count": {"type": "integer"}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/status/collectors": {
      "get": {
        "tags": ["status"],
        "summary": "List collectors",
        "description": "Returns collector runtime status classified by workflow coordinator registration, direct status evidence, and persisted source or reducer fact evidence.",
        "operationId": "listCollectors",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "List of collector runtimes",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string"},
                    "collectors": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "instance_id": {"type": "string"},
                          "collector_kind": {"type": "string"},
                          "mode": {"type": "string"},
                          "runtime_mode": {"type": "string"},
                          "status_category": {
                            "type": "string",
                            "enum": ["coordinator_managed", "direct_mode", "profile_gated", "disabled", "unregistered"]
                          },
                          "coordinator_registered": {"type": "boolean"},
                          "enabled": {"type": "boolean"},
                          "claims_enabled": {"type": "boolean"},
                          "evidence_sources": {"type": "array", "items": {"type": "string"}},
                          "source_systems": {"type": "array", "items": {"type": "string"}},
                          "observation_count": {"type": "integer"},
                          "health": {"type": "string"},
                          "detail": {"type": "string"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "classification_basis": {"type": "string"},
                    "updated_at": {"type": "string", "format": "date-time", "nullable": true}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/status/ingesters/{ingester}": {
      "get": {
        "tags": ["status"],
        "summary": "Get ingester status",
        "description": "Returns detailed status for a specific ingester.",
        "operationId": "getIngesterStatus",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "ingester",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Ingester name"
          }
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Ingester status",
            "content": {
              "application/json": {
                "schema": {"type": "object"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/ingesters": {
      "get": {
        "tags": ["status"],
        "summary": "List ingesters",
        "description": "Legacy compatibility alias for the Go-owned ingester status list.",
        "operationId": "listIngestersLegacy",
        "responses": {
          "200": {
            "description": "List of ingesters",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "ingesters": {"type": "array", "items": {"type": "object"}},
                    "count": {"type": "integer"}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/collectors": {
      "get": {
        "tags": ["status"],
        "summary": "List collectors",
        "description": "Legacy compatibility alias for collector runtime status.",
        "operationId": "listCollectorsLegacy",
        "responses": {
          "200": {
            "description": "List of collector runtimes",
            "content": {
              "application/json": {
                "schema": {"type": "object"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/ingesters/{ingester}": {
      "get": {
        "tags": ["status"],
        "summary": "Get ingester status",
        "description": "Legacy compatibility alias for the Go-owned ingester status detail route.",
        "operationId": "getIngesterStatusLegacy",
        "parameters": [
          {
            "name": "ingester",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Ingester name"
          }
        ],
        "responses": {
          "200": {
            "description": "Ingester status",
            "content": {
              "application/json": {
                "schema": {"type": "object"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/status/index": {
      "get": {
        "tags": ["status"],
        "summary": "Get index status",
        "description": "Returns the index status summary. Scoped tokens are admitted and receive a scoped shape instead of the deployment-wide report: only version, scoped=true, repository_count (counted over the caller's granted repositories and ingestion scopes inside the graph query, never an unfiltered count), completeness_state=scoped_repository_count_only, and withheld_sections naming status, reasons, queue, queue_blockages, coordinator, scope_activity, aws_materialization, semantic_extraction, and terraform_state. Those sections are process-global and cannot be attributed to a grant, and a queue_blockages row reports conflict_key as COALESCE(conflict_key, scope_id), a raw scope id, so they are withheld rather than redacted and the status snapshot is not read for a scoped caller. A scoped caller with no granted repository or ingestion scope receives repository_count 0 without a graph query. A failed or unavailable graph count returns 500 or 503 rather than 0. Callers holding the shared key receive the full report unchanged. A hosted BrowserSessionRoutePolicy that refuses all-scope callers on grant-bound routes refuses them here with a 403.",
        "operationId": "getIndexStatus",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Index status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string"},
                    "scoped": {"type": "boolean", "description": "true only on the scoped shape; absent for shared-key callers."},
                    "completeness_state": {"type": "string", "enum": ["scoped_repository_count_only"], "description": "Present only on the scoped shape."},
                    "withheld_sections": {"type": "array", "items": {"type": "string"}, "description": "Present only on the scoped shape: the deployment-wide sections not returned."},
                    "reasons": {"type": "array", "items": {"type": "string"}},
                    "repository_count": {"type": "integer"},
                    "queue": {"type": "object"},
                    "queue_blockages": {"type": "array", "items": {"type": "object"}},
                    "scope_activity": {"type": "object"},
                    "aws_materialization": {
                      "type": "object",
                      "properties": {
                        "outstanding": {"type": "integer"},
                        "pending": {"type": "integer"},
                        "in_flight": {"type": "integer"},
                        "blocked": {"type": "integer"},
                        "retrying": {"type": "integer"},
                        "dead_letter": {"type": "integer"},
                        "failed": {"type": "integer"},
                        "domains": {"type": "array", "items": {"type": "object"}}
                      }
                    },
                    "semantic_extraction": {
                      "type": "object",
                      "properties": {
                        "state": {"type": "string", "enum": ["unavailable", "available", "available_but_disabled_for_scope", "disabled_by_policy", "provider_unhealthy"]},
                        "reason": {"type": "string"},
                        "provider_configured": {"type": "boolean"},
                        "documentation_observations_enabled": {"type": "boolean"},
                        "code_hints_enabled": {"type": "boolean"},
                        "deterministic_paths_affected": {"type": "boolean"},
                        "deterministic_documentation_unblocked": {"type": "boolean"},
                        "provider_profiles": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "profile_id": {"type": "string"},
                              "display_name": {"type": "string"},
                              "provider_kind": {"type": "string", "enum": ["anthropic", "openai_compatible", "deepseek", "minimax", "gemini", "bedrock", "azure_openai", "ollama", "internal_gateway"]},
                              "credential_source_kind": {"type": "string", "enum": ["kubernetes_secret", "vault_secret_handle", "environment_variable", "cloud_workload_identity", "local_dev_profile"]},
                              "credential_configured": {"type": "boolean"},
                              "model_id": {"type": "string"},
                              "embedding_dimensions": {"type": "integer", "minimum": 1},
                              "endpoint_profile_id": {"type": "string"},
                              "source_classes": {"type": "array", "items": {"type": "string", "enum": ["documentation", "diagrams_images", "tickets_chat", "code_hints", "search_documents", "agent_reasoning"]}},
                              "source_policy_configured": {"type": "boolean"},
                              "state": {"type": "string", "enum": ["configured", "unconfigured", "healthy", "unhealthy"]},
                              "reason": {"type": "string"},
                              "detail": {"type": "string"},
                              "updated_at": {"type": "string", "format": "date-time"}
                            }
                          }
                        },
                        "detail": {"type": "string"},
                        "updated_at": {"type": "string", "format": "date-time"},
                        "supported_states": {"type": "array", "items": {"type": "string"}},
                        "supported_provider_profile_states": {"type": "array", "items": {"type": "string"}}
                      }
                    },
                    "terraform_state": {
                      "type": "object",
                      "properties": {
                        "warning_summary": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "warning_kind": {"type": "string"},
                              "reason": {"type": "string"},
                              "scope_class": {"type": "string"},
                              "severity": {"type": "string"},
                              "actionability": {"type": "string"},
                              "count": {"type": "integer"}
                            }
                          }
                        },
                        "recent_warnings": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "safe_locator_hash": {"type": "string"},
                              "backend_kind": {"type": "string"},
                              "warning_kind": {"type": "string"},
                              "reason": {"type": "string"},
                              "severity": {"type": "string"},
                              "actionability": {"type": "string"},
                              "source": {"type": "string"},
                              "source_handle": {"type": "string"}
                            }
                          }
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/index-status": {
      "get": {
        "tags": ["status"],
        "summary": "Get index status",
        "description": "Legacy compatibility alias for the Go-owned index status summary; it shares the handler with GET /api/v0/status/index. Scoped tokens are admitted and receive a scoped shape instead of the deployment-wide report: only version, scoped=true, repository_count (counted over the caller's granted repositories and ingestion scopes inside the graph query, never an unfiltered count), completeness_state=scoped_repository_count_only, and withheld_sections naming status, reasons, queue, queue_blockages, coordinator, scope_activity, aws_materialization, semantic_extraction, and terraform_state. Those sections are process-global and cannot be attributed to a grant, and a queue_blockages row reports conflict_key as COALESCE(conflict_key, scope_id), a raw scope id, so they are withheld rather than redacted and the status snapshot is not read for a scoped caller. A scoped caller with no granted repository or ingestion scope receives repository_count 0 without a graph query. A failed or unavailable graph count returns 500 or 503 rather than 0. Callers holding the shared key receive the full report unchanged. A hosted BrowserSessionRoutePolicy that refuses all-scope callers on grant-bound routes refuses them here with a 403.",
        "operationId": "getIndexStatusLegacy",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Index status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string"},
                    "scoped": {"type": "boolean", "description": "true only on the scoped shape; absent for shared-key callers."},
                    "completeness_state": {"type": "string", "enum": ["scoped_repository_count_only"], "description": "Present only on the scoped shape."},
                    "withheld_sections": {"type": "array", "items": {"type": "string"}, "description": "Present only on the scoped shape: the deployment-wide sections not returned."},
                    "reasons": {"type": "array", "items": {"type": "string"}},
                    "repository_count": {"type": "integer"},
                    "queue": {"type": "object"},
                    "queue_blockages": {"type": "array", "items": {"type": "object"}},
                    "scope_activity": {"type": "object"},
                    "aws_materialization": {
                      "type": "object",
                      "properties": {
                        "outstanding": {"type": "integer"},
                        "pending": {"type": "integer"},
                        "in_flight": {"type": "integer"},
                        "blocked": {"type": "integer"},
                        "retrying": {"type": "integer"},
                        "dead_letter": {"type": "integer"},
                        "failed": {"type": "integer"},
                        "domains": {"type": "array", "items": {"type": "object"}}
                      }
                    },
                    "semantic_extraction": {
                      "type": "object",
                      "properties": {
                        "state": {"type": "string", "enum": ["unavailable", "available", "available_but_disabled_for_scope", "disabled_by_policy", "provider_unhealthy"]},
                        "reason": {"type": "string"},
                        "provider_configured": {"type": "boolean"},
                        "documentation_observations_enabled": {"type": "boolean"},
                        "code_hints_enabled": {"type": "boolean"},
                        "deterministic_paths_affected": {"type": "boolean"},
                        "deterministic_documentation_unblocked": {"type": "boolean"},
                        "detail": {"type": "string"},
                        "updated_at": {"type": "string", "format": "date-time"},
                        "supported_states": {"type": "array", "items": {"type": "string"}}
                      }
                    },
                    "terraform_state": {
                      "type": "object",
                      "properties": {
                        "warning_summary": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "warning_kind": {"type": "string"},
                              "reason": {"type": "string"},
                              "scope_class": {"type": "string"},
                              "severity": {"type": "string"},
                              "actionability": {"type": "string"},
                              "count": {"type": "integer"}
                            }
                          }
                        },
                        "recent_warnings": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "safe_locator_hash": {"type": "string"},
                              "backend_kind": {"type": "string"},
                              "warning_kind": {"type": "string"},
                              "reason": {"type": "string"},
                              "severity": {"type": "string"},
                              "actionability": {"type": "string"},
                              "source": {"type": "string"},
                              "source_handle": {"type": "string"}
                            }
                          }
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/openapi.json": {
      "get": {
        "tags": ["health"],
        "summary": "OpenAPI specification",
        "description": "Returns the OpenAPI 3.0 specification for this API.",
        "operationId": "getOpenAPISpec",
        "responses": {
          "200": {
            "description": "OpenAPI specification",
            "content": {
              "application/json": {
                "schema": {"type": "object"}
              }
            }
          }
        }
      }
    },
`
