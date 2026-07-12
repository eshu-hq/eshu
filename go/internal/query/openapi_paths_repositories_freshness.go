// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsRepositoriesFreshness documents the repository freshness route
// (#5143). It is split from openAPIPathsRepositories to keep repository
// OpenAPI files small.
const openAPIPathsRepositoriesFreshness = `
    "/api/v0/repositories/{repo_id}/freshness": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get per-repository commit receipt and build-completeness verdict",
        "description": "Answers two questions for one repository: did eshu pick up its latest commit, and is the evidence for that commit fully built. verdict is one of current, building, behind, unobserved, or unknown. observed_commit may be an empty string -- this is legitimate for non-git scopes and for pre-delta-baseline git generations that predate the source_commit_sha column, and is represented explicitly rather than fabricated. The optional expected_commit query parameter is compared as an opaque string (no format validation); when it does not match observed_commit the verdict is behind regardless of whether a generation is actively progressing. shared_enrichment reports cross-repo materialization backlog referencing this repository's generation as a separate axis from stages, so a different repository's shared backlog is never attributed here. Scoped tokens receive the same shape; a repository outside the caller's grant 404s like sibling repository routes.",
        "operationId": "getRepositoryFreshness",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"},
          {
            "name": "expected_commit",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional commit SHA the caller expects to be observed. When supplied and it does not match observed_commit, the verdict is behind."
          }
        ],
        "responses": {
          "200": {
            "description": "Repository freshness verdict",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repository": {"$ref": "#/components/schemas/RepositoryRef"},
                    "scope_id": {"type": "string"},
                    "verdict": {"type": "string", "enum": ["current", "building", "behind", "unobserved", "unknown"]},
                    "observed_commit": {"type": "string", "description": "May be empty for non-git scopes or pre-delta-baseline generations."},
                    "observed_at": {"type": "string", "nullable": true},
                    "generation": {
                      "type": "object",
                      "nullable": true,
                      "properties": {
                        "id": {"type": "string"},
                        "status": {"type": "string"},
                        "trigger_kind": {"type": "string"},
                        "is_delta": {"type": "boolean"},
                        "activated_at": {"type": "string", "nullable": true}
                      }
                    },
                    "stages": {
                      "type": "object",
                      "properties": {
                        "collected": {"type": "boolean"},
                        "reduced": {"type": "boolean"},
                        "projected": {"type": "boolean"},
                        "materialized": {"type": "boolean"}
                      }
                    },
                    "outstanding_by_stage": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "stage": {"type": "string"},
                          "status": {"type": "string"},
                          "count": {"type": "integer"}
                        }
                      }
                    },
                    "shared_enrichment": {
                      "type": "object",
                      "properties": {
                        "pending": {"type": "boolean"},
                        "pending_domains": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "domain": {"type": "string"},
                              "count": {"type": "integer"}
                            }
                          }
                        }
                      }
                    },
                    "unobserved_push": {
                      "type": "object",
                      "nullable": true,
                      "properties": {
                        "target_sha": {"type": "string"},
                        "ref": {"type": "string"},
                        "received_at": {"type": "string", "nullable": true}
                      }
                    },
                    "as_of": {"type": "string"},
                    "scoped": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {
            "description": "Repository freshness reader not configured",
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
