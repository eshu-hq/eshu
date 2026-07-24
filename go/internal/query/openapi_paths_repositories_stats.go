// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsRepositoriesStats documents the repository stats route. It is
// split from openAPIPathsRepositories to keep repository OpenAPI files small.
const openAPIPathsRepositoriesStats = `
    "/api/v0/repositories/{repo_id}/stats": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository statistics",
        "description": "Returns timeout-bounded repository statistics from content-store coverage when available. Counts are null and coverage.missing_evidence explains the gap when the read model is unavailable or times out; the handler does not fall back to whole-graph traversal. The response carries the canonical truth envelope plus an additive result_limits drilldown block and an explicit partial_reasons slot; the existing coverage partial_results/truncated/timeout fields are preserved. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryStats",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "200": {
            "description": "Repository statistics",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef"},
                    "file_count": {"type": "integer", "nullable": true},
                    "languages": {"type": "array", "items": {"type": "string"}},
                    "entity_count": {"type": "integer", "nullable": true},
                    "entity_types": {"type": "array", "items": {"type": "string"}},
                    "coverage": {
                      "type": "object",
                      "properties": {
                        "source_backend": {"type": "string", "enum": ["content_store", "unavailable"]},
                        "query_shape": {"type": "string", "enum": ["content_store_repository_coverage", "repository_identity_only"]},
                        "counts_available": {"type": "boolean"},
                        "entity_types_available": {"type": "boolean"},
                        "whole_graph_traversal": {"type": "boolean"},
                        "partial_results": {"type": "boolean"},
                        "truncated": {"type": "boolean"},
                        "timeout": {"type": "boolean"},
                        "timeout_budget": {"type": "string"},
                        "missing_evidence": {"type": "array", "items": {"type": "string"}},
                        "file_count_source": {"type": "string"},
                        "entity_count_source": {"type": "string"},
                        "languages_source": {"type": "string"},
                        "entity_types_source": {"type": "string"},
                        "content_last_indexed_at": {"type": "string"},
                        "last_error": {"type": "string"}
                      }
                    },
                    "result_limits": {"type": "object", "description": "Additive drilldown block: bounded language/entity-type limit, deterministic ordering, language and entity-type counts, truncation flag, and the get_repository_coverage drilldown plus stats context path.", "additionalProperties": true},
                    "partial_reasons": {"type": "array", "description": "Explicit missing-evidence and timeout reasons for the stats read; always present so the envelope shape is stable across complete and partial reads.", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "504": {
            "description": "Repository stats read timed out before repository identity could be verified",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
