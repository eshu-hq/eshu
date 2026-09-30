// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// Symbols is the OpenAPI path fragment documenting the 5 `/api/v0/code/*`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Symbols = `
    "/api/v0/code/symbols/search": {
      "post": {
        "tags": ["code"],
        "summary": "Find symbol definitions",
        "description": "Finds exact or fuzzy symbol definitions using bounded, paged content-index lookups. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "findSymbolDefinitions",
        "x-scoped-token-support": true,
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
        "description": "Returns bounded content-index structural inventory for functions, classes, top-level file elements, dataclasses, documented functions, decorated methods, classes with a method, super calls, and function counts per file. Requests must include at least one scope filter: repo_id, file_path, language, entity_kind, or symbol. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "inspectCodeInventory",
        "x-scoped-token-support": true,
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                    "results": {"type": "array", "description": "Inventory rows. Entity rows carry source_cache clipped at read time to 4096 bytes: a clipped row adds source_cache_clipped, source_cache_clip_bytes, and source_cache_total_bytes, and source_handle locates the full body for get_entity_content or get_file_lines. The docstring, and every echo derived from it, is clipped the same way to 512 bytes: a clipped row adds docstring_clipped, docstring_clip_bytes, and docstring_total_bytes.", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": "integer", "nullable": true},
                    "source_backend": {"type": "string"},
                    "source_cache_clip_bytes": {"type": "integer", "description": "Read-time source_cache ceiling in bytes (4096) applied to every row of this response; always present."},
                    "source_cache_clipped_rows": {"type": "integer", "description": "Number of returned rows whose source_cache was clipped; 0 when none."},
                    "docstring_clip_bytes": {"type": "integer", "description": "Read-time docstring ceiling in bytes (512) applied to every row of this response; always present."},
                    "docstring_clipped_rows": {"type": "integer", "description": "Number of returned rows whose docstring was clipped; 0 when none."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/imports/investigate": {
      "post": {
        "tags": ["code"],
        "summary": "Investigate import and module dependencies",
        "description": "Returns bounded graph-backed import dependencies, package imports, bounded simple Python file-import cycles, and cross-module calls. Cycles enumerate rotation-deduplicated simple cycles up to max_cycle_length (default 5) over all stored IMPORTS edges with no type-only or deferred exclusion; enumeration stops at 1000 cycles or a fixed step budget and reports truncated:true with the stop reason; page on has_more and next_offset. Requests must include at least one scope filter: repo_id, source_file, target_file, source_module, or target_module. target_file is accepted only for file_import_cycles and cross_module_calls. Internal candidate scans are capped at 25000 rows and return 422 with an instruction to narrow scope when that bound is exceeded. The row payload uses one canonical key by query type: dependencies, modules, cycles, or cross_module_calls. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with 400.",
        "operationId": "investigateImportDependencies",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "query_type": {
                    "type": "string",
                    "enum": ["imports_by_file", "importers", "module_dependencies", "package_imports", "file_import_cycles", "cross_module_calls"],
                    "default": "imports_by_file"
                  },
                  "repo_id": {"type": "string", "description": "Optional repository selector (canonical ID, name, slug, or path)."},
                  "language": {"type": "string", "description": "Optional language filter. file_import_cycles supports python multi-node cycle detection; other languages are rejected."},
                  "max_cycle_length": {"type": "integer", "description": "Simple-cycle length bound for file_import_cycles (default 5). Ignored by other query types.", "default": 5, "minimum": 2, "maximum": 8},
                  "source_file": {"type": "string", "description": "Optional repo-relative source file path anchor"},
                  "target_file": {"type": "string", "description": "Optional repo-relative target file path for cross-module call and cycle queries"},
                  "source_module": {"type": "string", "description": "Optional source module name anchor"},
                  "target_module": {"type": "string", "description": "Optional imported or target module name anchor"},
                  "limit": {"type": "integer", "default": 25, "minimum": 0, "maximum": 200},
                  "offset": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Import dependency investigation results",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "query_type": {"type": "string"},
                    "scope": {"type": "object", "additionalProperties": true},
                    "dependencies": {"type": "array", "description": "Canonical rows for imports_by_file, importers, and module_dependencies query_type values. One row per (file, module) pair, which is the identity of the underlying File-[:IMPORTS]->Module edge. imported_name and alias are populated only when every import statement joining that file to that module agrees on them: a file importing two symbols from one module returns the row with both fields empty rather than naming an arbitrary one of the two.", "items": {"type": "object", "additionalProperties": true}},
                    "modules": {"type": "array", "description": "Canonical rows for package_imports query_type.", "items": {"type": "object", "additionalProperties": true}},
                    "cycles": {
                      "type": "array",
                      "description": "Canonical rows for file_import_cycles query_type: bounded simple Python import cycles ordered length-ascending, then normalized path. cycle_length counts edges; cycle_path closes back on its first file; cycle_edges carries one IMPORTS proof edge per hop. Enumeration stops at 1000 cycles or at a fixed step budget, whichever comes first, and reports truncated:true with coverage.cycle_enumeration_cap, coverage.cycle_enumeration_step_budget, and coverage.cycle_enumeration_stop_reason.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "repo_id": {"type": "string"},
                          "repo_name": {"type": "string"},
                          "source_file": {"type": "string"},
                          "target_file": {"type": "string"},
                          "source_module": {"type": "string"},
                          "target_module": {"type": "string"},
                          "source_line_number": {"type": "integer"},
                          "back_edge_line_number": {"type": "integer"},
                          "relationship_type": {"type": "string", "enum": ["IMPORTS"]},
                          "cycle_length": {"type": "integer", "description": "Number of import edges in the cycle (2 through max_cycle_length)."},
                          "cycle_path": {"type": "array", "items": {"type": "string"}},
                          "cycle_edges": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "properties": {
                                "relationship_type": {"type": "string", "enum": ["IMPORTS"]},
                                "source_file": {"type": "string"},
                                "target_file": {"type": "string"},
                                "source_module": {"type": "string"},
                                "target_module": {"type": "string"},
                                "line_number": {"type": "integer"}
                              }
                            }
                          },
                          "source_handle": {"type": "object", "additionalProperties": true}
                        },
                        "additionalProperties": true
                      }
                    },
                    "cross_module_calls": {"type": "array", "description": "Canonical rows for cross_module_calls query_type.", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean", "description": "True when the answer is partial: another page exists, or (for file_import_cycles) the cycle enumeration stopped early at the 1000-cycle cap or the step budget. A capped run stays truncated on every page, including the last. Page on has_more and next_offset, not on truncated."},
                    "has_more": {"type": "boolean", "description": "True only while another page of this enumeration exists. It is false on the last page even when truncated is true."},
                    "next_offset": {"type": "integer", "nullable": true, "description": "The offset to request next, or null when has_more is false."},
                    "source_backend": {"type": "string"},
                    "coverage": {"type": "object", "additionalProperties": true, "description": "Bounds and completeness. For file_import_cycles it carries cycle_max_length, cycle_enumeration_cap, cycle_enumeration_truncated, cycle_enumeration_stop_reason (none, cycle_cap, or step_budget), and cycle_enumeration_step_budget."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "422": {
            "description": "The candidate scan exceeded the internal bound; narrow the repository, file, or module scope.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/call-graph/metrics": {
      "post": {
        "tags": ["code"],
        "summary": "Inspect call graph metrics",
        "description": "Returns exact graph-backed call graph metrics for recursive functions and highly connected hub functions when the repository has at most 50,000 physical CALLS edges. Requests require repo_id and use deterministic ordering, paging, truncation metadata, source handles, and one canonical functions row key. Larger scopes fail closed with HTTP 422 and no partial metric rows. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "inspectCallGraphMetrics",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id"],
                "properties": {
                  "metric_type": {
                    "type": "string",
                    "enum": ["hub_functions", "recursive_functions"],
                    "default": "hub_functions"
                  },
                  "repo_id": {"type": "string", "description": "Required repository selector (canonical ID, name, slug, or path)."},
                  "language": {"type": "string", "description": "Optional language filter."},
                  "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 200},
                  "offset": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Call graph metric rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "metric_type": {"type": "string"},
                    "scope": {"type": "object", "additionalProperties": true},
                    "functions": {"type": "array", "description": "Canonical call graph metric rows.", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": "integer", "nullable": true},
                    "source_backend": {"type": "string"},
                    "coverage": {"type": "object", "additionalProperties": true}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "422": {
            "description": "The repository has more than 50,000 physical CALLS edges and exceeds the exact metric bound. No partial metric rows are returned.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/topics/investigate": {
      "post": {
        "tags": ["code"],
        "summary": "Investigate a code topic",
        "description": "Finds ranked files and symbols for a broad natural-language code topic using one bounded content-index query. Returns coverage, truncation, source handles, and exact next-call handles for source reads and relationship stories. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "investigateCodeTopic",
        "x-scoped-token-support": true,
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
                    "candidate_pool_truncated": {"type": "boolean", "description": "True when the bounded per-term candidate pool reached its cap for at least one search term (#7008); the corpus may have additional, lower-ranked matches that never entered scoring."},
                    "source_backend": {"type": "string"},
                    "coverage": {"type": "object", "additionalProperties": true},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer metadata with schema_version, evidence_handles, missing_evidence, limitations, truncated, coverage, partial_reasons, and recommended_next_calls."}
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
