// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCode = `
    "/api/v0/code/search": {
      "post": {
        "tags": ["code"], "summary": "Search code entities",
        "description": "Searches code entities by case-sensitive name. Repository-selected requests use the indexed graph path. Global requests use the current content entity name index; global substring requests require at least 3 Unicode characters.",
        "operationId": "searchCode",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["query"],
                "properties": {
                  "query": {"type": "string", "description": "Case-sensitive entity name or substring. Global substring searches require at least 3 Unicode characters."},
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)"},
                  "language": {"type": "string", "description": "Optional language filter"},
                  "limit": {"type": "integer", "description": "Maximum returned page size (default 50, maximum 200)", "default": 50, "minimum": 1, "maximum": 200},
                  "exact": {"type": "boolean", "description": "When true, require a complete case-sensitive entity-name match. Exact global searches may be shorter than 3 characters.", "default": false}
                }
              }
            }
          }
        },
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Search results",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/CodeSearchResponse"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/language-query": {
      "post": {
        "tags": ["code"],
        "summary": "Query entities by language and type",
        "description": "Queries graph-backed or content-backed entities for one language/entity-type pair.",
        "operationId": "queryLanguageEntities",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["language", "entity_type"],
                "properties": {
                  "language": {"type": "string", "description": "Language family to query"},
                  "entity_type": {
                    "type": "string",
                    "description": "Entity type to query",
                    "enum": [
                      "repository",
                      "directory",
                      "file",
                      "module",
                      "function",
                      "class",
                      "struct",
                      "enum",
                      "union",
                      "macro",
                      "variable",
                      "annotation",
                      "protocol",
                      "impl_block",
                      "type_alias",
                      "type_annotation",
                      "typedef",
                      "component",
                      "terraform_module",
                      "terragrunt_config",
                      "terragrunt_dependency",
                      "terragrunt_local",
                      "terragrunt_input",
                      "sql_table",
                      "sql_view",
                      "sql_function",
                      "sql_trigger",
                      "sql_index",
                      "sql_migration",
                      "sql_column"
                    ]
                  },
                  "query": {"type": "string", "description": "Optional name filter"},
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)"},
                  "limit": {"type": "integer", "description": "Max results (default 50)", "default": 50}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Language query results",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/LanguageQueryResponse"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/relationships": {
      "post": {
        "tags": ["code"],
        "summary": "Get code relationships",
        "description": "Returns incoming and outgoing relationships for an entity.",
        "operationId": "getCodeRelationships",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "anyOf": [
                  {"required": ["entity_id"]},
                  {"required": ["name"]}
                ],
                "properties": {
                  "entity_id": {"type": "string"},
                  "name": {
                    "type": "string",
                    "description": "Optional entity name fragment when entity_id is not available."
                  },
                  "direction": {
                    "type": "string",
                    "enum": ["incoming", "outgoing"],
                    "description": "Optional relationship direction filter."
                  },
                  "relationship_type": {
                    "type": "string",
                    "description": "Optional relationship type filter such as CALLS, IMPORTS, or REFERENCES."
                  },
                  "transitive": {
                    "type": "boolean",
                    "description": "When true, traverse transitive CALLS relationships instead of only one hop."
                  },
                  "max_depth": {
                    "type": "integer",
                    "description": "Maximum traversal depth for transitive CALLS lookups (default 5, max 10).",
                    "default": 5
                  }
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Entity relationships",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "entity_id": {"type": "string"},
                    "name": {"type": "string"},
                    "labels": {"type": "array", "items": {"type": "string"}},
                    "file_path": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "repo_name": {"type": "string"},
                    "language": {"type": "string"},
                    "start_line": {"type": "integer"},
                    "end_line": {"type": "integer"},
                    "metadata": {"type": "object", "additionalProperties": true},
                    "outgoing": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}},
                    "incoming": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}},
                    "outgoing_truncated": {"type": "boolean", "description": "True when the outgoing direct relationships exceeded the per-direction row ceiling and the returned set was clipped."},
                    "incoming_truncated": {"type": "boolean", "description": "True when the incoming direct relationships exceeded the per-direction row ceiling and the returned set was clipped."}
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
    "/api/v0/code/relationships/story": {
      "post": {
        "tags": ["code"],
        "summary": "Get a bounded code relationship story",
        "description": "Resolves one target symbol or entity id, returns ambiguity candidates instead of guessing, and reads direct or bounded transitive relationships with deterministic pagination.",
        "operationId": "getCodeRelationshipStory",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "anyOf": [{"required": ["target"]}, {"required": ["entity_id"]}, {"required": ["query_type", "repo_id"], "properties": {"query_type": {"enum": ["overrides"]}}}],
                "properties": {
                  "query_type": {"type": "string", "enum": ["class_hierarchy", "overrides"], "description": "Optional story enrichment mode for class hierarchy or override prompts."},
                  "target": {"type": "string", "description": "Symbol name to resolve when entity_id is omitted."},
                  "name": {"type": "string", "description": "Alias for target."},
                  "entity_id": {"type": "string", "description": "Canonical entity id to anchor the relationship query."},
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path) for name resolution."},
                  "cross_repo": {"type": "boolean", "description": "Explicit opt-in for bounded cross-repository traversal. Cross-repo relationship-story requests require repo_id as the anchor repository selector, including exact entity_id requests.", "default": false},
                  "language": {"type": "string", "description": "Optional language filter for name resolution."},
                  "direction": {"type": "string", "enum": ["incoming", "outgoing", "both"], "default": "both"},
                  "relationship_type": {"type": "string", "enum": ["CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO"], "default": "CALLS"},
                  "relationship_types": {"type": "array", "items": {"type": "string", "enum": ["CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO"]}, "description": "Optional additive multi-type filter; supersedes relationship_type and merges each type's bounded results. Not supported with include_transitive, class_hierarchy, or overrides."},
                  "include_transitive": {"type": "boolean", "description": "When true, follows CALLS edges with bounded breadth-first traversal.", "default": false},
                  "max_depth": {"type": "integer", "description": "Maximum transitive CALLS or class hierarchy depth (default 5, max 10).", "default": 5, "maximum": 10},
                  "limit": {"type": "integer", "description": "Maximum relationship rows or ambiguity candidates (default 25, max 200).", "default": 25, "maximum": 200},
                  "offset": {"type": "integer", "description": "Zero-based direct relationship offset.", "default": 0, "maximum": 10000},
                  "min_confidence": {"type": "number", "description": "Optional confidence floor from 0 through 1. Omitted preserves low-confidence and missing-confidence rows; positive values keep only returned rows with numeric confidence at or above the floor.", "minimum": 0, "maximum": 1},
                  "token_budget": {"type": "integer", "description": "Optional cap on the estimated response token cost. Applied after limit; trims rows to fit and reports what was cut with guidance to narrow.", "minimum": 0}
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Relationship story",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "target_resolution": {"type": "object", "additionalProperties": true},
                    "scope": {"type": "object", "additionalProperties": true},
                    "relationships": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}},
                    "class_hierarchy": {"type": "object", "additionalProperties": true},
                    "override_story": {"type": "object", "additionalProperties": true},
                    "summary": {"type": "object", "additionalProperties": true},
                    "coverage": {"type": "object", "additionalProperties": true, "properties": {
                      "missing_edge_reason": {"type": "string", "enum": ["complete", "target_unresolved", "no_relationships_found", "all_below_confidence_floor", "truncated_by_limit", "truncated_by_token_budget"], "description": "Why the result is empty or short, so an agent need not guess. Descriptive only; never changes the answer's truth label."},
                      "truncation_state": {"type": "string", "enum": ["none", "count", "token_budget", "count_and_token_budget"], "description": "Whether and how the result was capped."},
                      "evidence_explanation": {"type": "string", "description": "Bounded human-readable explanation of the missing-edge reason."}
                    }},
                    "source_backend": {"type": "string"}
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
    "/api/v0/code/call-chain": {
      "post": {
        "tags": ["code"],
        "summary": "Find transitive call chains",
        "description": "Finds shortest call chains between two functions by following canonical CALLS edges.",
        "operationId": "getCodeCallChain",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "start": {"type": "string", "description": "Exact caller function name when start_entity_id is omitted"},
                  "end": {"type": "string", "description": "Exact callee function name when end_entity_id is omitted"},
                  "start_entity_id": {"type": "string", "description": "Canonical caller entity id. Takes precedence over start when provided."},
                  "end_entity_id": {"type": "string", "description": "Canonical callee entity id. Takes precedence over end when provided."},
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path) to scope both endpoints to one repository."},
                  "cross_repo": {"type": "boolean", "description": "Explicit opt-in for bounded cross-repository call-chain traversal.", "default": false},
                  "start_repo_id": {"type": "string", "description": "Optional starting repository selector for cross-repo name resolution or endpoint verification."},
                  "end_repo_id": {"type": "string", "description": "Optional ending repository selector for cross-repo name resolution or endpoint verification."},
                  "max_depth": {"type": "integer", "description": "Maximum traversal depth (default 5, max 10)", "default": 5}
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Call chain results",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "start": {"type": "string"},
                    "end": {"type": "string"},
                    "start_entity_id": {"type": "string"},
                    "end_entity_id": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "cross_repo": {"type": "boolean"},
                    "start_repo_id": {"type": "string"},
                    "end_repo_id": {"type": "string"},
                    "chains": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "depth": {"type": "integer"},
                          "chain": {
                            "type": "array",
                            "items": {"$ref": "#/components/schemas/EntityRef"}
                          }
                        }
                      }
                    }
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
    "/api/v0/code/complexity": {
      "post": {
        "tags": ["code"],
        "summary": "Get complexity metrics",
        "description": "Returns relationship-based complexity metrics for an entity or a bounded list of the most complex functions.",
        "operationId": "getComplexity",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "entity_id": {"type": "string"},
                  "function_name": {"type": "string"},
                  "repo_id": {"type": "string"},
                  "limit": {"type": "integer", "default": 10, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Complexity metrics",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "entity_id": {"type": "string"},
                    "name": {"type": "string"},
                    "labels": {"type": "array", "items": {"type": "string"}},
                    "file_path": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "repo_name": {"type": "string"},
                    "language": {"type": "string"},
                    "start_line": {"type": "integer"},
                    "end_line": {"type": "integer"},
                    "metadata": {"type": "object", "additionalProperties": true},
                    "outgoing_count": {"type": "integer"},
                    "incoming_count": {"type": "integer"},
                    "total_relationships": {"type": "integer"},
                    "results": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "409": {"description": "Ambiguous function name; retry with entity_id from error.details.candidates"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
