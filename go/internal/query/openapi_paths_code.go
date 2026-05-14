package query

const openAPIPathsCode = `
    "/api/v0/code/search": {
      "post": {
        "tags": ["code"],
        "summary": "Search code entities",
        "description": "Searches code entities by name pattern or content.",
        "operationId": "searchCode",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["query"],
                "properties": {
                  "query": {"type": "string", "description": "Search pattern"},
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)"},
                  "language": {"type": "string", "description": "Optional language filter"},
                  "limit": {"type": "integer", "description": "Max results (default 50)", "default": 50}
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
                "schema": {"$ref": "#/components/schemas/CodeSearchResponse"}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/cypher": {
      "post": {
        "tags": ["code"],
        "summary": "Run bounded read-only Cypher",
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
                    "incoming": {"type": "array", "items": {"$ref": "#/components/schemas/Relationship"}}
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
                "anyOf": [
                  {"required": ["target"]},
                  {"required": ["entity_id"]}
                ],
                "properties": {
                  "target": {"type": "string", "description": "Symbol name to resolve when entity_id is omitted."},
                  "name": {"type": "string", "description": "Alias for target."},
                  "entity_id": {"type": "string", "description": "Canonical entity id to anchor the relationship query."},
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path) for name resolution."},
                  "language": {"type": "string", "description": "Optional language filter for name resolution."},
                  "direction": {"type": "string", "enum": ["incoming", "outgoing", "both"], "default": "both"},
                  "relationship_type": {"type": "string", "enum": ["CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES"], "default": "CALLS"},
                  "include_transitive": {"type": "boolean", "description": "When true, follows CALLS edges with bounded breadth-first traversal.", "default": false},
                  "max_depth": {"type": "integer", "description": "Maximum transitive CALLS depth (default 5, max 10).", "default": 5, "maximum": 10},
                  "limit": {"type": "integer", "description": "Maximum relationship rows or ambiguity candidates (default 25, max 200).", "default": 25, "maximum": 200},
                  "offset": {"type": "integer", "description": "Zero-based direct relationship offset.", "default": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
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
                    "summary": {"type": "object", "additionalProperties": true},
                    "coverage": {"type": "object", "additionalProperties": true},
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
                  "max_depth": {"type": "integer", "description": "Maximum traversal depth (default 5, max 10)", "default": 5}
                }
              }
            }
          }
        },
        "responses": {
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
    "/api/v0/code/dead-code": {
      "post": {
        "tags": ["code"],
        "summary": "Find dead code",
        "description": "Finds graph-backed dead-code candidates, applies the current default entrypoint/test/generated exclusions plus modeled language roots such as Go public-package exports, C parser-backed roots, C# parser-backed roots, Dart parser-backed roots, Haskell parser-backed roots, Kotlin parser-backed roots, Elixir parser-backed roots, Perl parser-backed roots, PHP parser-backed roots, and Groovy Jenkins roots, and classifies returned candidates without changing the derived truth envelope.",
        "operationId": "findDeadCode",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)"},
                  "language": {"type": "string", "description": "Optional parser language filter. Use this when validating one language family, for example csharp, c, dart, haskell, kotlin, elixir, perl, php, groovy, or sql."},
                  "limit": {"type": "integer", "description": "Maximum dead-code candidates to return (default 100, max 500).", "default": 100},
                  "exclude_decorated_with": {
                    "type": "array",
                    "description": "Optional list of decorator names to exclude from the results.",
                    "items": {"type": "string"}
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Dead code candidates",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_id": {"type": "string"},
                    "language": {"type": "string"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean", "description": "True when either displayed results were clipped to limit or the bounded paged raw candidate scan reached its scan cap."},
                    "display_truncated": {"type": "boolean", "description": "True when filtered display results exceeded limit and were clipped."},
                    "candidate_scan_truncated": {"type": "boolean", "description": "True when the paged raw candidate scan reached candidate_scan_limit before exhausting candidates."},
                    "candidate_scan_limit": {"type": "integer", "description": "Maximum raw candidate rows the bounded dead-code scan may inspect before policy exclusions."},
                    "candidate_scan_pages": {"type": "integer", "description": "Number of raw candidate pages read before returning results."},
                    "candidate_scan_rows": {"type": "integer", "description": "Number of raw candidate rows inspected before policy exclusions."},
                    "results": {
                      "type": "array",
                      "items": {
                        "allOf": [
                          {"$ref": "#/components/schemas/EntityRef"},
                          {
                            "type": "object",
                            "properties": {
                              "classification": {
                                "type": "string",
                                "description": "Per-result dead-code classification. Returned graph candidates are classified without upgrading the envelope truth level.",
                                "enum": ["unused", "reachable", "excluded", "ambiguous", "derived_candidate_only", "unsupported_language"]
                              }
                            }
                          }
                        ]
                      }
                    },
                    "analysis": {
                      "type": "object",
                      "properties": {
                        "root_categories_used": {"type": "array", "items": {"type": "string"}},
                        "frameworks_recognized": {"type": "array", "items": {"type": "string"}},
                        "reflection_modeled": {"type": "boolean"},
                        "tests_excluded": {"type": "boolean"},
                        "generated_code_excluded": {"type": "boolean"},
                        "user_overrides_applied": {"type": "boolean"},
                        "dead_code_language_maturity": {"type": "object", "additionalProperties": {"type": "string"}},
                        "dead_code_language_exactness_blockers": {
                          "type": "object",
                          "description": "Named blockers that prevent exact cleanup-safe dead-code truth for a language.",
                          "additionalProperties": {"type": "array", "items": {"type": "string"}}
                        },
                        "dead_code_observed_exactness_blockers": {
                          "type": "object",
                          "description": "Named exactness blockers observed on returned candidates, grouped by language.",
                          "additionalProperties": {"type": "array", "items": {"type": "string"}}
                        },
                        "modeled_entrypoints": {"type": "array", "items": {"type": "string"}},
                        "modeled_public_api": {"type": "array", "items": {"type": "string"}},
                        "notes": {"type": "array", "items": {"type": "string"}}
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
        "description": "Returns relationship-based complexity metrics for an entity.",
        "operationId": "getComplexity",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["entity_id"],
                "properties": {
                  "entity_id": {"type": "string"},
                  "repo_id": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
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
                    "total_relationships": {"type": "integer"}
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
