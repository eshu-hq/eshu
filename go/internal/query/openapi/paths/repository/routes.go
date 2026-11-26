// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// Routes is the OpenAPI path fragment documenting its routes, among them
// `/api/v0/catalog`, `/api/v0/repositories`,
// `/api/v0/repositories/by-language`. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
//
// The context and story routes live in Context and Story (same package) to
// keep this file under the source size cap.
const Routes = `
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
                    "status": {"type": "string", "example": "ok", "description": "Health status emitted by the API health handler (always ok on a live instance)."}
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
        "description": "Returns a bounded page of indexed repositories. This route also serves the inventory (empty-selector) form of get_repository_stats, so the response carries an additive result_limits drilldown block and an explicit partial_reasons slot alongside the existing truncated paging field. The optional name filter is a case-insensitive substring matched against the repository name or repo slug before count and page; with name present total counts the filtered set. The optional projection selects the row shape: full (the default here) returns every row, compact returns identity rows only (id, name, repo_slug, index_state) for large pages.",
        "operationId": "listRepositories",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 100, "minimum": 1, "maximum": 500}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}},
          {"name": "name", "in": "query", "schema": {"type": "string", "maxLength": 200}, "description": "Optional case-insensitive substring filter matched against repository name or repo slug. Empty or whitespace-only means absent."},
          {"name": "projection", "in": "query", "schema": {"type": "string", "enum": ["full", "compact"], "default": "full"}, "description": "Row shape. Compact returns identity rows only (id, name, repo_slug, index_state). Defaults to full on this route."}
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
                      "description": "Bounded page of repository rows, emitted by the repository list handler as shared Repository rows (full projection) or identity rows (compact projection).",
                      "items": {"$ref": "#/components/schemas/Repository"}
                    },
                    "count": {"type": "integer", "description": "Number of repositories returned in this page."},
                    "total": {"type": "integer", "description": "True total repository count independent of page size. Use this field — not count — to display the overall repository count on dashboards and sidebars. With the name filter present, counts the filtered set: omit name for the full inventory count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the repository list handler from the normalized limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the repository list handler from the normalized offset."},
                    "truncated": {"type": "boolean", "description": "True when more repositories exist past this page, emitted by the repository list handler from the lookahead row."},
                    "projection": {"type": "string", "enum": ["full", "compact"], "description": "Row shape served for this page: full rows or compact identity rows."},
                    "result_limits": {"type": "object", "description": "Additive drilldown block for the inventory form of get_repository_stats: bounded page limit/offset, deterministic ordering, repository count, truncation flag, and the get_repository_stats drilldown plus inventory context path.", "additionalProperties": true},
                    "partial_reasons": {"type": "array", "description": "Explicit reasons the response is partial. repository_inventory_truncated means more repositories exist beyond this page (mirrors truncated above). repository_group_evidence_missing means at least one returned repository has no source-backed grouping evidence. dependency_marker_evidence_incomplete means the bounded DEPENDS_ON edge read backing each repository's is_dependency field (and its dependency-cluster grouping) failed or was truncated for this request; is_dependency may be under-reported (false where evidence was actually true) on affected rows, and this reason is independent of truncated -- it never means more repositories exist beyond the page. Always present (possibly empty) so the envelope shape is stable.", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                    "language": {"type": "string", "description": "Echo of the caller's language family selector, emitted by the repository language handler from the normalized language param."},
                    "normalized_languages": {"type": "array", "items": {"type": "string"}, "description": "Language aliases the selector expanded to (e.g. typescript includes tsx), emitted by the repository language handler from the normalized language family."},
                    "repository_count": {"type": "integer", "description": "Repositories containing files of this language family, emitted by the repository language handler from the language aggregate."},
                    "file_count": {"type": "integer", "description": "Files of this language family, emitted by the repository language handler from the language aggregate."},
                    "last_indexed_at": {"type": "string", "format": "date-time", "description": "Newest index timestamp across the family, emitted by the repository language handler from the language aggregate."},
                    "repositories": {
                      "type": "array",
                      "description": "Bounded page of repositories in this language family, emitted by the repository language handler from repositoryLanguageRepositoryMaps as shared Repository rows extended with per-language counts.",
                      "items": {
                        "allOf": [
                          {"$ref": "#/components/schemas/Repository"},
                          {
                            "type": "object",
                            "properties": {
                              "file_count": {"type": "integer", "description": "Family file count for this repository, emitted by repository.repositoryLanguageRepositoryMaps."},
                              "languages": {
                                "type": "array",
                                "description": "Per-language file counts for this repository, emitted by repository.repositoryLanguageRepositoryMaps from coverageLanguageMaps.",
                                "items": {
                                  "type": "object",
                                  "description": "One per-language file count. Owner: repository.repositoryLanguageRepositoryMaps.",
                                  "properties": {
                                    "language": {"type": "string", "description": "Language name emitted by the repository language coverage maps."},
                                    "file_count": {"type": "integer", "description": "Files of this language emitted by the repository language coverage maps."}
                                  },
                                  "required": ["language"]
                                }
                              },
                              "last_indexed_at": {"type": "string", "format": "date-time", "description": "Index timestamp for this repository, emitted by repository.repositoryLanguageRepositoryMaps."}
                            }
                          }
                        ]
                      }
                    },
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the repository language handler from the normalized limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the repository language handler from the normalized offset."},
                    "truncated": {"type": "boolean", "description": "True when more repositories exist past this page, emitted by the repository language handler from the lookahead row."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
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
                      "description": "Aggregate repository and file counts per indexed language bucket, emitted by the repository language handler from repositoryLanguageInventoryMaps.",
                      "items": {
                        "type": "object",
                        "description": "One language bucket row. Owner: repository.repositoryLanguageInventoryMaps.",
                        "properties": {
                          "language": {"type": "string", "description": "Language bucket name emitted by repository.repositoryLanguageInventoryMaps."},
                          "repository_count": {"type": "integer", "description": "Repositories containing this language, emitted by repository.repositoryLanguageInventoryMaps."},
                          "file_count": {"type": "integer", "description": "Files of this language, emitted by repository.repositoryLanguageInventoryMaps."},
                          "last_indexed_at": {"type": "string", "format": "date-time", "description": "Newest index timestamp in this bucket, emitted by repository.repositoryLanguageInventoryMaps."}
                        },
                        "required": ["language", "repository_count", "file_count"]
                      }
                    },
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the repository language handler from the normalized limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the repository language handler from the normalized offset."},
                    "truncated": {"type": "boolean", "description": "True when more language buckets exist past this page, emitted by the repository language handler from the lookahead row."}
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
    "/api/v0/catalog": {
      "get": {
        "tags": ["catalog"],
        "summary": "List catalog entities",
        "description": "Returns bounded repository, workload, and service handles for catalog navigation. Scoped tokens receive rows intersected with the caller's granted repositories; same-name sibling counts exclude workloads outside the grant.",
        "operationId": "listCatalog",
        "x-scoped-token-support": true,
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Catalog entity handles",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repositories": {
                      "type": "array",
                      "description": "Bounded repository handles for catalog navigation, emitted by the repository catalog handler (component shape).",
                      "items": {"$ref": "#/components/schemas/Repository"}
                    },
                    "workloads": {
                      "type": "array",
                      "description": "Bounded workload handles for catalog navigation, emitted by the repository catalog handler (component shape).",
                      "items": {"$ref": "#/components/schemas/CatalogWorkload"}
                    },
                    "services": {
                      "type": "array",
                      "description": "Bounded service handles for catalog navigation, emitted by the repository catalog handler (component shape).",
                      "items": {"$ref": "#/components/schemas/CatalogWorkload"}
                    },
                    "counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Row counts per catalog collection, emitted by the repository catalog handler from catalog counts. Keys are the collection names (repositories, workloads, services); values are the row counts in this page."},
                    "count": {"type": "integer", "description": "Rows from the repositories and workloads collections returned in this page, emitted by the repository catalog handler via catalogResponse.finish; the services rows on the same page are excluded from this count (see counts)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page bound, emitted by the repository catalog handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when any catalog collection (repositories or workloads/services) is itself a bounded partial page, i.e. more rows exist beyond limit. A degraded is_dependency read (see limitations) never sets this: the repository/workload rows returned are complete even when the auxiliary dependency marker on them is not."},
                    "workloads_truncated": {"type": "boolean", "description": "True only when the workload and service collections are a bounded partial page; repository-only truncation does not set this field."},
                    "limitations": {"type": "array", "description": "Explicit caveats about this response's completeness or truth basis. dependency_marker_evidence_incomplete means the bounded DEPENDS_ON edge read backing each repository's is_dependency field failed or was truncated for this request; is_dependency may be under-reported (false where evidence was actually true) on affected rows. \"workload and service catalog rows require an authoritative graph backend\" appears when no graph backend is configured. Distinct from truncated, which is about row-count paging, not evidence completeness.", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
` + Context + Story + `
    "/api/v0/repositories/{repo_id}/tree": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository file tree",
        "description": "Lists one directory level (or the full subtree with recursive=true) reconstructed from the content-store file index. The 50,000-file cap applies after path and language selection; truncated means the matching subtree exceeds the cap. Returns directory and file entries; child_count on a directory is the number of descendant files. The ref reflects the indexed commit SHA the tree was built from. When ref is supplied, it must resolve to the indexed commit; known but unindexed refs return 409 instead of silently falling back. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryTree",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"},
          {"name": "path", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Directory subpath to list, relative to the repository root. An exact file path returns 404, including with a language filter."},
          {"name": "ref", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Branch name or commit SHA selector. The request succeeds only when the selector resolves to the indexed commit; unavailable or unindexed refs return an error."},
          {"name": "recursive", "in": "query", "required": false, "schema": {"type": "boolean"}, "description": "When true, return the full subtree instead of a single directory level."},
          {"name": "language", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Filter the listing to files of this language/source-type (e.g. go, python, hcl, yaml). Aliases expand to a family: typescript also matches tsx; terraform also matches hcl/tfvars. A path with no matching files returns an empty listing, not 404."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Repository file tree",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "ref": {"type": "string", "description": "Indexed commit SHA the tree was built from, emitted by the repository tree handler."},
                    "path": {"type": "string", "description": "Echo of the caller's directory subpath, emitted by the repository tree handler from the normalized path."},
                    "truncated": {"type": "boolean", "description": "True when the matching subtree exceeds the file cap, emitted by the repository tree handler from the bounded read."},
                    "entries": {
                      "type": "array",
                      "description": "Directory and file entries for the listed level (or full subtree with recursive=true), emitted by the repository tree handler from the content-store file index.",
                      "items": {
                        "type": "object",
                        "description": "One directory or file entry. Owner: repository tree handler file index.",
                        "properties": {
                          "name": {"type": "string", "description": "Entry name emitted by the repository tree handler from the file index."},
                          "type": {"type": "string", "enum": ["dir", "file"], "description": "Entry kind emitted by the repository tree handler from the file index."},
                          "path": {"type": "string", "description": "Repository-relative entry path emitted by the repository tree handler from the file index."},
                          "size": {"type": "integer", "description": "File byte length emitted by the repository tree handler from the file index (zero for directories)."},
                          "language": {"type": "string", "description": "Entry language emitted by the repository tree handler from the file index (present when known)."},
                          "child_count": {"type": "integer", "description": "Descendant file count for a directory, emitted by the repository tree handler from the file index (present on directories only)."}
                        },
                        "required": ["name", "type", "path"]
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Repository file content",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "path": {"type": "string", "description": "Echo of the caller's repository-relative file path, emitted by the repository content handler."},
                    "ref": {"type": "string", "description": "Indexed commit SHA the bytes were read from, emitted by the repository content handler."},
                    "encoding": {"type": "string", "enum": ["utf-8", "base64"], "description": "Content encoding emitted by the repository content handler (utf-8 text, or base64 for non-UTF-8 bytes)."},
                    "content": {"type": "string", "description": "File bytes as text or base64, emitted by the repository content handler from the content store (never redacted-collector content)."},
                    "size": {"type": "integer", "description": "Original byte length emitted by the repository content handler from the content store."},
                    "language": {"type": "string", "description": "File language emitted by the repository content handler from the file index (present when known)."},
                    "truncated": {"type": "boolean", "description": "True when the byte cap was reached, emitted by the repository content handler from the bounded read."}
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
`
