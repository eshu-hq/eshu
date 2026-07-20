// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsRepositoriesBranches documents the repository branches/refs route.
// It is a separate const from openAPIPathsRepositories to keep that file under
// the source size cap.
const openAPIPathsRepositoriesBranches = `
    "/api/v0/repositories/{repo_id}/branches": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository refs",
        "description": "Returns source-backed Git branches and tags captured during repository ingestion for the console branch selector. Repositories without source ref metadata keep the legacy single indexed commit fallback with an empty branch name and default_branch so no branch names are invented.",
        "operationId": "getRepositoryBranches",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"}
        ],
        "responses": {
          "200": {
            "description": "Repository refs",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "default_branch": {"type": "string"},
                    "branches": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "name": {"type": "string"},
                          "kind": {"type": "string"},
                          "head_sha": {"type": "string"},
                          "is_default": {"type": "boolean"},
                          "observed_at": {"type": "string", "format": "date-time"},
                          "last_indexed_at": {"type": "string", "format": "date-time"}
                        }
                      }
                    },
                    "tags": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "name": {"type": "string"},
                          "kind": {"type": "string"},
                          "head_sha": {"type": "string"},
                          "observed_at": {"type": "string", "format": "date-time"},
                          "last_indexed_at": {"type": "string", "format": "date-time"}
                        }
                      }
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
`
