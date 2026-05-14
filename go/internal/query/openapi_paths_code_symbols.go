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
                "anyOf": [
                  {"required": ["symbol"]},
                  {"required": ["query"]}
                ],
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/topics/investigate": {
      "post": {
        "tags": ["code"],
        "summary": "Investigate a code topic",
        "description": "Finds ranked files and symbols for a broad natural-language code topic using one bounded content-index query. Returns coverage, truncation, source handles, and exact next-call handles for source reads and relationship stories.",
        "operationId": "investigateCodeTopic",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "anyOf": [
                  {"required": ["topic"]},
                  {"required": ["query"]}
                ],
                "properties": {
                  "topic": {"type": "string", "description": "Natural-language topic or behavior to investigate"},
                  "query": {"type": "string", "description": "Compatibility alias for topic"},
                  "intent": {"type": "string", "description": "Optional caller intent such as explain_flow or debug_issue"},
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)"},
                  "language": {"type": "string", "description": "Optional language filter"},
                  "limit": {"type": "integer", "default": 25, "maximum": 200},
                  "offset": {"type": "integer", "default": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Ranked topic evidence and follow-up handles",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "topic": {"type": "string"},
                    "intent": {"type": "string"},
                    "scope": {"type": "object", "additionalProperties": true},
                    "searched_terms": {"type": "array", "items": {"type": "string"}},
                    "matched_files": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "matched_symbols": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "evidence_groups": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "call_graph_handles": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "source_backend": {"type": "string"},
                    "coverage": {"type": "object", "additionalProperties": true}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
