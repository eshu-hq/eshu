package query

const openAPIPathsCodeSymbols = `
    "/api/v0/code/symbols/search": {
      "post": {
        "tags": ["code"],
        "summary": "Find symbol definitions",
        "description": "Finds exact or fuzzy symbol definitions using bounded, paged content-index lookups.",
        "operationId": "findSymbolDefinitions",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["symbol"],
                "properties": {
                  "symbol": {"type": "string", "description": "Symbol name to locate"},
                  "query": {"type": "string", "description": "Compatibility alias for symbol"},
                  "match_mode": {"type": "string", "enum": ["exact", "fuzzy"], "default": "exact"},
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)"},
                  "language": {"type": "string", "description": "Optional language filter"},
                  "entity_type": {"type": "string", "description": "Optional single entity type filter"},
                  "entity_types": {"type": "array", "items": {"type": "string"}, "description": "Optional entity type filters"},
                  "limit": {"type": "integer", "default": 25, "maximum": 200},
                  "offset": {"type": "integer", "default": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Symbol definition results",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/SymbolSearchResponse"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
