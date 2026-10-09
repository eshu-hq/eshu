// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

// Content is the OpenAPI path fragment documenting the 5 `/api/v0/content/*`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Content = `
    "/api/v0/content/files/read": {
      "post": {
        "tags": ["content"],
        "summary": "Read file content",
        "description": "Reads full file content from the content store.",
        "operationId": "readFile",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id", "relative_path"],
                "properties": {
                  "repo_id": {"type": "string"},
                  "relative_path": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "File content",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/FileContent"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/content/files/lines": {
      "post": {
        "tags": ["content"],
        "summary": "Read file lines",
        "description": "Reads a line range from a file.",
        "operationId": "readFileLines",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id", "relative_path", "start_line", "end_line"],
                "properties": {
                  "repo_id": {"type": "string"},
                  "relative_path": {"type": "string"},
                  "start_line": {"type": "integer"},
                  "end_line": {"type": "integer"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "File lines",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/FileContent"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/content/entities/read": {
      "post": {
        "tags": ["content"],
        "summary": "Read entity content",
        "description": "Reads entity source code from the content store.",
        "operationId": "readEntity",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["entity_id"],
                "properties": {
                  "entity_id": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Entity content",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/EntityContent"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/content/files/search": {
      "post": {
        "tags": ["content"],
        "summary": "Search file content",
        "description": "Searches file content by pattern. A search with no repository filter runs inside a work budget (ESHU_CONTENT_SEARCH_BUDGET_MS, default 800 ms): when the budget ends before the page is proven complete the answer is still HTTP 200, with truncated true, a partial object (reason, rows_scanned_in_order, rows_matched, cursor, budget_ms, elapsed_ms, overrun_ms, progressed, hint) and truth level partial. The returned rows are an ordered prefix of the exact answer; resume with the cursor request field and offset max(0, offset - rows_matched) while progressed is true, or add repo_id to scope the search. A partial with progressed false scanned nothing inside the budget: resuming repeats the same request, so scope with repo_id, ask the operator to raise the server budget (ESHU_CONTENT_SEARCH_BUDGET_MS), or stop, and bound any resume loop.",
        "operationId": "searchFiles",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "anyOf": [
                  {"required": ["query"]},
                  {"required": ["pattern"]}
                ],
                "properties": {
                  "repo_id": {"type": "string"},
                  "repo_ids": {
                    "type": "array",
                    "items": {"type": "string"},
                    "description": "Optional repository selector list. Explicit multi-repo search is executed as one bounded PostgreSQL query."
                  },
                  "query": {"type": "string"},
                  "pattern": {
                    "type": "string",
                    "description": "Alias for query used by MCP content-search tools."
                  },
                  "limit": {"type": "integer", "default": 50, "maximum": 200},
                  "offset": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000},
                  "cursor": {
                    "type": "object",
                    "description": "Resumes a search with no repository filter strictly after this key. Use the cursor of a previous partial result. Refused (400) on any search that has a repository filter.",
                    "properties": {
                      "repo_id": {"type": "string"},
                      "relative_path": {"type": "string"}
                    }
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Search results",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "results": {"type": "array", "items": {"$ref": "#/components/schemas/FileContent"}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean", "description": "True when another matching row follows the page, and always true when partial is present."},
                    "partial": {
                      "type": "object",
                      "description": "Present only when a search with no repository filter ended at its work budget before the page was proven complete. results is then an ordered prefix of the exact answer.",
                      "properties": {
                        "reason": {"type": "string", "enum": ["candidate_budget_exceeded", "budget_exceeded_on_large_document"]},
                        "rows_scanned_in_order": {"type": "integer", "description": "Rows this call visited in repo_id, relative_path order, up to and including cursor; a resumed call counts from its request cursor."},
                        "rows_matched": {"type": "integer", "description": "Matching rows found so far, before the request offset is applied."},
                        "cursor": {
                          "type": "object",
                          "properties": {
                            "repo_id": {"type": "string"},
                            "relative_path": {"type": "string"}
                          }
                        },
                        "budget_ms": {"type": "integer", "description": "The requested work budget."},
                        "elapsed_ms": {"type": "integer", "description": "SQL wall time the server measured."},
                        "overrun_ms": {"type": "integer", "description": "How far a cancelled statement ran past its own timeout; above 100 the reason is budget_exceeded_on_large_document."},
                        "progressed": {"type": "boolean", "description": "True when this call advanced the cursor past the request cursor. False means it scanned nothing inside the budget and resuming repeats the identical request."},
                        "hint": {"type": "string"}
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/content/entities/search": {
      "post": {
        "tags": ["content"],
        "summary": "Search entity content",
        "description": "Searches entity source code by pattern. Pages order by repository ID, relative path, start line, and entity ID; truncated reports whether another matching row follows.",
        "operationId": "searchEntities",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "anyOf": [
                  {"required": ["query"]},
                  {"required": ["pattern"]}
                ],
                "properties": {
                  "repo_id": {"type": "string"},
                  "repo_ids": {
                    "type": "array",
                    "items": {"type": "string"},
                    "description": "Optional repository selector list. Explicit multi-repo search is executed as one bounded PostgreSQL query."
                  },
                  "query": {"type": "string"},
                  "pattern": {
                    "type": "string",
                    "description": "Alias for query used by MCP content-search tools."
                  },
                  "limit": {"type": "integer", "default": 50, "maximum": 200},
                  "offset": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Search results",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/EntityContentSearchResponse"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
