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
    "/api/v0/code/structure/inventory": {
      "post": {
        "tags": ["code"],
        "summary": "Inspect structural code inventory",
        "description": "Returns bounded content-index structural inventory for functions, classes, top-level file elements, dataclasses, documented functions, decorated methods, classes with a method, super calls, and function counts per file. Requests must include at least one scope filter: repo_id, file_path, language, entity_kind, or symbol.",
        "operationId": "inspectCodeInventory",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path). One of repo_id, file_path, language, entity_kind, or symbol is required."},
                  "language": {"type": "string", "description": "Optional language filter"},
                  "inventory_kind": {
                    "type": "string",
                    "enum": ["entity", "top_level", "dataclass", "documented", "documented_function", "decorated", "class_with_method", "super_call", "function_count_by_file"],
                    "default": "entity"
                  },
                  "entity_kind": {"type": "string", "description": "Optional entity kind such as function, class, module, variable, component, type_alias, or sql_function. Must be function for function_count_by_file inventory."},
                  "file_path": {"type": "string", "description": "Optional repo-relative file path"},
                  "symbol": {"type": "string", "description": "Optional exact entity name"},
                  "decorator": {"type": "string", "description": "Optional decorator filter"},
                  "method_name": {"type": "string", "description": "Method name required for class_with_method inventory"},
                  "class_name": {"type": "string", "description": "Optional class or implementation context filter"},
                  "limit": {"type": "integer", "default": 25, "maximum": 200},
                  "offset": {"type": "integer", "default": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Structural inventory results",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_id": {"type": "string"},
                    "language": {"type": "string"},
                    "inventory_kind": {"type": "string"},
                    "entity_kind": {"type": "string"},
                    "results": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "matches": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": "integer", "nullable": true},
                    "source_backend": {"type": "string"}
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
