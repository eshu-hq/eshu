package query

const openAPIPathsContent = `
    "/api/v0/content/files/read": {
      "post": {
        "tags": ["content"],
        "summary": "Read file content",
        "description": "Reads full file content from the content store.",
        "operationId": "readFile",
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
          "404": {"$ref": "#/components/responses/NotFound"},
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
          "404": {"$ref": "#/components/responses/NotFound"},
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
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/content/files/search": {
      "post": {
        "tags": ["content"],
        "summary": "Search file content",
        "description": "Searches file content by pattern.",
        "operationId": "searchFiles",
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
                "schema": {
                  "type": "object",
                  "properties": {
                    "results": {"type": "array", "items": {"$ref": "#/components/schemas/FileContent"}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/content/entities/search": {
      "post": {
        "tags": ["content"],
        "summary": "Search entity content",
        "description": "Searches entity source code by pattern.",
        "operationId": "searchEntities",
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
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
