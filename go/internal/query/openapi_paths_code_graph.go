package query

const openAPIPathsCodeGraph = `
    "/api/v0/code/cypher": {
      "post": {
        "tags": ["code"], "summary": "Run bounded read-only Cypher",
        "description": "Diagnostics-only graph query endpoint. Prefer purpose-built code, service, and impact routes for prompt contracts. Queries are read-only, timeout-bound, and server-capped.",
        "operationId": "runReadOnlyCypher",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["cypher_query"],
                "properties": {
                  "cypher_query": {"type": "string"},
                  "limit": {"type": "integer", "default": 100, "maximum": 1000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Bounded Cypher results",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "results": {"type": "array", "items": {"type": "object"}},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/bundles": {
      "post": {
        "tags": ["code"],
        "summary": "Search indexed bundle candidates",
        "description": "Searches indexed repositories as pre-indexed bundle candidates. This route does not upload bundle archives or mutate graph state.",
        "operationId": "searchCodeBundles",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["query"],
                "properties": {
                  "query": {"type": "string", "description": "Bundle search text"},
                  "limit": {"type": "integer", "description": "Max results (default 10)", "default": 10}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Bundle candidates",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "bundles": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer"}
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
`
