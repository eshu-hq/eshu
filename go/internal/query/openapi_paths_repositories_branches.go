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
        "description": "Returns source-backed Git branches and tags captured during repository ingestion for the console branch selector, bounded by a single limit+cursor over the combined branches+tags stream ordered by (kind: all branches precede all tags, then name). With no params the response defaults to limit=100 -- the endpoint always bounds its response, never returning the full unbounded ref list. default_branch (and each branch entry's is_default) identify the default branch, not its position in the paged stream -- the default branch can appear anywhere among branches[] depending on its name. All branch entries precede all tag entries across the paged stream; a page containing a tag implies no further branches remain. Repositories without source ref metadata keep the legacy single indexed commit fallback with an empty branch name and default_branch, always truncated:false, so no branch names are invented.",
        "operationId": "getRepositoryBranches",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"},
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100},
            "description": "Maximum combined branches+tags entries to return in this page. Defaults to 100 when omitted; must be in [1, 500]."
          },
          {
            "name": "cursor",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Opaque forward-only keyset cursor from a previous response's next_cursor. Encodes the last-emitted ref's sort key (kind, name) -- deliberately excluding is_default, which can change between page fetches -- tolerating ref churn (additions, deletions, or a default-branch change) between pages without skipping or duplicating entries. An unparseable, wrong-version, wrong-kind, or cross-repository cursor returns 400."
          }
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
                    },
                    "truncated": {
                      "type": "boolean",
                      "description": "Always present. True when more refs exist beyond this page; next_cursor is then present too."
                    },
                    "next_cursor": {
                      "type": "string",
                      "description": "Opaque forward-only cursor for the next page. Present only when truncated is true."
                    },
                    "tags_truncated": {
                      "type": "boolean",
                      "description": "Deprecated in favor of truncated/next_cursor. True when more tags exist beyond what tags[] carries in this page; omitted when false."
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
