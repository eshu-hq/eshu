// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// StatsAndCoverage documents the two per-repository
// content-store coverage readbacks, GET /api/v0/repositories/{repo_id}/stats
// and GET /api/v0/repositories/{repo_id}/coverage. Both are split from
// Routes to keep repository OpenAPI files small; coverage
// moved here when declaring its policy 403 pushed that file over the 500-line
// cap.
const StatsAndCoverage = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "200": {
            "description": "Repository statistics",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef", "description": "Repository identity emitted by the repository stats handler from the base row (component shape)."},
                    "file_count": {"type": "integer", "nullable": true, "description": "Indexed file count emitted by the repository stats handler from the content coverage (null when unavailable)."},
                    "languages": {"type": "array", "items": {"type": "string"}, "description": "Language families emitted by the repository stats handler from the content coverage."},
                    "entity_count": {"type": "integer", "nullable": true, "description": "Indexed entity count emitted by the repository stats handler from the content coverage (null when unavailable)."},
                    "entity_types": {"type": "array", "items": {"type": "string"}, "description": "Entity types emitted by the repository stats handler from the content coverage."},
                    "coverage": {
                      "type": "object",
                      "description": "Coverage provenance for the counts, emitted by the repository stats handler from repositoryStatsCoverageMap.",
                      "properties": {
                        "source_backend": {"type": "string", "enum": ["content_store", "unavailable"], "description": "Backend that served the counts, emitted by repository.repositoryStatsCoverageMap."},
                        "query_shape": {"type": "string", "enum": ["content_store_repository_coverage", "repository_identity_only"], "description": "Query shape that served the counts, emitted by repository.repositoryStatsCoverageMap."},
                        "counts_available": {"type": "boolean", "description": "Whether counts are available, emitted by repository.repositoryStatsCoverageMap."},
                        "entity_types_available": {"type": "boolean", "description": "Whether entity types are available, emitted by repository.repositoryStatsCoverageMap."},
                        "whole_graph_traversal": {"type": "boolean", "description": "Whether whole-graph traversal ran, emitted by repository.repositoryStatsCoverageMap (always false)."},
                        "partial_results": {"type": "boolean", "description": "Whether the results are partial, emitted by repository.repositoryStatsCoverageMap."},
                        "truncated": {"type": "boolean", "description": "Whether the coverage read truncated, emitted by repository.repositoryStatsCoverageMap."},
                        "timeout": {"type": "boolean", "description": "Whether the coverage read timed out, emitted by repository.repositoryStatsCoverageMap."},
                        "timeout_budget": {"type": "string", "description": "Coverage read timeout budget emitted by repository.repositoryStatsCoverageMap from repositoryStatsReadTimeout."},
                        "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Missing evidence classes emitted by repository.repositoryStatsCoverageMap."},
                        "file_count_source": {"type": "string", "description": "File count provenance emitted by repository.repositoryStatsCoverageMap."},
                        "entity_count_source": {"type": "string", "description": "Entity count provenance emitted by repository.repositoryStatsCoverageMap."},
                        "languages_source": {"type": "string", "description": "Languages provenance emitted by repository.repositoryStatsCoverageMap."},
                        "entity_types_source": {"type": "string", "description": "Entity types provenance emitted by repository.repositoryStatsCoverageMap."},
                        "content_last_indexed_at": {"type": "string", "description": "Last content index timestamp emitted by repository.repositoryStatsCoverageMap (present when the coverage read succeeded)."},
                        "last_error": {"type": "string", "description": "Present when counts are unavailable. One fixed string from a closed set, never backend error text: content store coverage exceeded 2s route timeout, content store coverage read failed (missing_evidence names content_store_coverage_error), content store not configured, or content store coverage unavailable. A reader fence on the coverage read answers 503 and a client cancel 499 instead of this partial."}
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
          "409": {"$ref": "#/components/responses/Conflict"},
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Repository coverage",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_id": {"type": "string", "description": "Echo of the caller's repository, emitted by the repository coverage handler from the normalized repo id."},
                    "completeness_state": {"type": "string", "description": "Coverage completeness verdict emitted by the repository coverage handler from completenessStateForCoverage."},
                    "graph_available": {"type": "boolean", "description": "Whether graph coverage stats were available, emitted by the repository coverage handler from the graph stats read."},
                    "server_content_available": {"type": "boolean", "description": "Whether content-store coverage was available, emitted by the repository coverage handler from the content coverage read."},
                    "graph_gap_count": {"type": "integer", "description": "Graph-side gap count emitted by the repository coverage handler."},
                    "content_gap_count": {"type": "integer", "description": "Content-side gap count emitted by the repository coverage handler."},
                    "file_count": {"type": "integer", "description": "Indexed file count emitted by the repository coverage handler from the content coverage (zero when unavailable)."},
                    "entity_count": {"type": "integer", "description": "Indexed entity count emitted by the repository coverage handler from the content coverage (zero when unavailable)."},
                    "content_last_indexed_at": {"type": "string", "description": "Last content index timestamp emitted by the repository coverage handler from the content coverage (empty when unavailable)."},
                    "last_error": {"type": "string", "description": "One of two fixed strings, never backend error text: graph coverage stats skipped because content store coverage is available, or content store not available (the graph coverage stats were read instead). A failed coverage read answers an error status instead. Emitted by the repository coverage handler."},
                    "languages": {
                      "type": "array",
                      "description": "Per-language file counts emitted by the repository coverage handler from taxonomy.CoverageLanguageMaps.",
                      "items": {
                        "type": "object",
                        "description": "One per-language file count. Owner: taxonomy.CoverageLanguageMaps.",
                        "properties": {
                          "language": {"type": "string", "description": "Language name emitted by taxonomy.CoverageLanguageMaps."},
                          "file_count": {"type": "integer", "description": "Files of this language emitted by taxonomy.CoverageLanguageMaps."}
                        },
                        "required": ["language", "file_count"]
                      }
                    },
                    "summary": {
                      "type": "object",
                      "description": "Graph-vs-content coverage summary emitted by the repository coverage handler from the bounded coverage map.",
                      "properties": {
                        "graph_file_count": {"type": "integer", "description": "Graph-side file count emitted by the repository coverage handler from the graph stats."},
                        "graph_entity_count": {"type": "integer", "description": "Graph-side entity count emitted by the repository coverage handler from the graph stats."},
                        "content_file_count": {"type": "integer", "description": "Content-side file count emitted by the repository coverage handler from the content coverage."},
                        "content_entity_count": {"type": "integer", "description": "Content-side entity count emitted by the repository coverage handler from the content coverage."},
                        "content_files_last_indexed_at": {"type": "string", "description": "Content files index timestamp emitted by the repository coverage handler."},
                        "content_entities_last_indexed_at": {"type": "string", "description": "Content entities index timestamp emitted by the repository coverage handler."},
                        "graph_gap_count": {"type": "integer", "description": "Graph-side gap count repeated in the summary, emitted by the repository coverage handler (present when content coverage is available)."},
                        "content_gap_count": {"type": "integer", "description": "Content-side gap count repeated in the summary, emitted by the repository coverage handler (present when content coverage is available)."},
                        "completeness_state": {"type": "string", "description": "Coverage completeness verdict repeated in the summary, emitted by the repository coverage handler (present when content coverage is available)."}
                      },
                      "required": ["graph_file_count", "graph_entity_count", "content_file_count", "content_entity_count"]
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
