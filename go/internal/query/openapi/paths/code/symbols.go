// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// Symbols is the OpenAPI path fragment documenting the `/api/v0/code/*`
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
                  "description": "Structural code inventory page emitted by handleStructuralInventory.",
                  "properties": {
                    "repo_id": {"type": "string", "description": "Repository selector echoed by handleStructuralInventory."},
                    "language": {"type": "string", "description": "Language filter echoed by handleStructuralInventory."},
                    "inventory_kind": {"type": "string", "description": "Inventory kind echoed by handleStructuralInventory."},
                    "entity_kind": {"type": "string", "description": "Entity kind echoed by handleStructuralInventory."},
                    "results": {"type": "array", "description": "Inventory rows. Entity rows carry source_cache clipped at read time to 4096 bytes: a clipped row adds source_cache_clipped, source_cache_clip_bytes, and source_cache_total_bytes, and source_handle locates the full body for get_entity_content or get_file_lines. The docstring, and every echo derived from it, is clipped the same way to 512 bytes: a clipped row adds docstring_clipped, docstring_clip_bytes, and docstring_total_bytes.", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer", "description": "Rows on this page counted by handleStructuralInventory."},
                    "limit": {"type": "integer", "description": "Page bound echoed by handleStructuralInventory."},
                    "offset": {"type": "integer", "description": "Page offset echoed by handleStructuralInventory."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, per handleStructuralInventory."},
                    "next_offset": {"type": "integer", "description": "Next offset to request when truncated is true, per handleStructuralInventory.", "nullable": true},
                    "source_backend": {"type": "string", "description": "Backend that served the inventory, per handleStructuralInventory."},
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
        "description": "Returns bounded graph-backed import dependencies, package imports, bounded simple Python file-import cycles, and cross-module calls. Cycles enumerate rotation-deduplicated simple cycles up to max_cycle_length (default 5) after excluding type-only and deferred IMPORTS edges, labelling each cycle runtime, ambiguous (an inferred edge), or flags_unknown (an edge written before the import flags existed); enumeration stops at 1000 cycles or a fixed step budget and reports truncated:true with the stop reason; page on has_more and next_offset. Requests must include at least one scope filter: repo_id, source_file, target_file, source_module, target_module, or target_module_prefix. target_file is accepted only for file_import_cycles and cross_module_calls. target_module_prefix matches Module names by prefix and is mutually exclusive with target_module; an explicitly empty prefix is rejected. external_only drops relative and absolute specifiers on the module-matching reads. Internal candidate scans are capped at 25000 rows and return 422 with an instruction to narrow scope when that bound is exceeded. The row payload uses one canonical key by query type: dependencies, modules, cycles, or cross_module_calls. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with 400.",
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
                  "language": {"type": "string", "description": "Optional language filter. file_import_cycles supports python multi-node cycle detection, go package cycle detection, typescript/tsx/javascript package cycle detection, and java package cycle detection; other languages are rejected."},
                  "max_cycle_length": {"type": "integer", "description": "Simple-cycle length bound for file_import_cycles (default 5). Ignored by other query types.", "default": 5, "minimum": 2, "maximum": 8},
                  "source_file": {"type": "string", "description": "Optional repo-relative source file path anchor"},
                  "target_file": {"type": "string", "description": "Optional repo-relative target file path for cross-module call and cycle queries"},
                  "source_module": {"type": "string", "description": "Optional source module name anchor"},
                  "target_module": {"type": "string", "description": "Optional imported or target module name anchor"},
                  "target_module_prefix": {"type": "string", "description": "Optional module-name prefix anchor (STARTS WITH); mutually exclusive with target_module, rejected when empty, counts as a scope filter"},
                  "external_only": {"type": "boolean", "description": "Exclude relative and absolute specifiers, keeping bare package names; applies only to the module-matching reads"},
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
                  "description": "Import dependency investigation page emitted by handleImportDependencyInvestigation.",
                  "properties": {
                    "query_type": {"type": "string", "description": "Query type echoed by handleImportDependencyInvestigation."},
                    "scope": {"type": "object", "description": "Resolved investigation scope keyed by scope field names, per handleImportDependencyInvestigation.", "additionalProperties": true},
                    "dependencies": {"type": "array", "description": "Canonical rows for imports_by_file, importers, and module_dependencies query_type values. One row per (file, module) pair, which is the identity of the underlying File-[:IMPORTS]->Module edge. imported_name and alias are populated only when every import statement joining that file to that module agrees on them: a file importing two symbols from one module returns the row with both fields empty rather than naming an arbitrary one of the two.", "items": {"type": "object", "additionalProperties": true}},
                    "modules": {"type": "array", "description": "Canonical rows for package_imports query_type.", "items": {"type": "object", "additionalProperties": true}},
                    "cycles": {
                      "type": "array",
                      "description": "Canonical rows for file_import_cycles query_type: bounded simple Python import cycles ordered length-ascending, then normalized path. cycle_length counts edges; cycle_path closes back on its first file; cycle_edges carries one IMPORTS proof edge per hop. Enumeration stops at 1000 cycles or at a fixed step budget, whichever comes first, and reports truncated:true with coverage.cycle_enumeration_cap, coverage.cycle_enumeration_step_budget, and coverage.cycle_enumeration_stop_reason.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "repo_id": {"type": "string", "description": "Repository id of the cycle, per handleImportDependencyInvestigation."},
                          "repo_name": {"type": "string", "description": "Repository name of the cycle, per handleImportDependencyInvestigation."},
                          "source_file": {"type": "string", "description": "Cycle source file, per handleImportDependencyInvestigation."},
                          "target_file": {"type": "string", "description": "Cycle target file, per handleImportDependencyInvestigation."},
                          "source_module": {"type": "string", "description": "Cycle source module, per handleImportDependencyInvestigation."},
                          "target_module": {"type": "string", "description": "Cycle target module, per handleImportDependencyInvestigation."},
                          "source_line_number": {"type": "integer", "description": "Source import line number, per handleImportDependencyInvestigation."},
                          "back_edge_line_number": {"type": "integer", "description": "Back-edge line number, per handleImportDependencyInvestigation."},
                          "relationship_type": {"type": "string", "description": "Always IMPORTS for cycle rows, per handleImportDependencyInvestigation.", "enum": ["IMPORTS"]},
                          "cycle_length": {"type": "integer", "description": "Number of import edges in the cycle (2 through max_cycle_length)."},
                          "cycle_path": {"type": "array", "description": "Cycle file path closing back on its first file, per handleImportDependencyInvestigation.", "items": {"type": "string"}},
                          "cycle_label": {"type": "string", "enum": ["runtime", "ambiguous", "flags_unknown"], "description": "runtime: every edge is a proven load-time import. ambiguous: at least one edge is inferred (a guessed target). flags_unknown: no edge is inferred and at least one has no flag properties because it was written before the import flags existed; the cycle may include an import that never runs."},
                          "cycle_edges": {
                            "type": "array",
                            "description": "One IMPORTS proof edge per cycle hop, per handleImportDependencyInvestigation.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "relationship_type": {"type": "string", "description": "Always IMPORTS for cycle edges, per handleImportDependencyInvestigation.", "enum": ["IMPORTS"]},
                                "source_file": {"type": "string", "description": "Edge source file, per handleImportDependencyInvestigation."},
                                "target_file": {"type": "string", "description": "Edge target file, per handleImportDependencyInvestigation."},
                                "source_module": {"type": "string", "description": "Edge source module, per handleImportDependencyInvestigation."},
                                "target_module": {"type": "string", "description": "Edge target module, per handleImportDependencyInvestigation."},
                                "line_number": {"type": "integer", "description": "Edge line number, per handleImportDependencyInvestigation."},
                                "flag_state": {"type": "string", "enum": ["runtime", "inferred", "unknown"], "description": "runtime: the projector wrote all three import flags and none is set. inferred: the parser synthesized the import's source. unknown: a flag property is missing or is not a boolean, which is how an edge written before the flags existed reads. Type-only and deferred edges are excluded before the walk and never appear here."}
                              }
                            }
                          },
                          "source_handle": {"type": "object", "description": "Source locator for the cycle, per handleImportDependencyInvestigation.", "additionalProperties": true}
                        },
                        "additionalProperties": true
                      }
                    },
                    "cross_module_calls": {"type": "array", "description": "Canonical rows for cross_module_calls query_type.", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer", "description": "Rows on this page counted by handleImportDependencyInvestigation."},
                    "limit": {"type": "integer", "description": "Page bound echoed by handleImportDependencyInvestigation."},
                    "offset": {"type": "integer", "description": "Page offset echoed by handleImportDependencyInvestigation."},
                    "truncated": {"type": "boolean", "description": "True when the answer is partial: another page exists, or (for file_import_cycles) the cycle enumeration stopped early at the 1000-cycle cap or the step budget. A capped run stays truncated on every page, including the last. Page on has_more and next_offset, not on truncated."},
                    "has_more": {"type": "boolean", "description": "True only while another page of this enumeration exists. It is false on the last page even when truncated is true."},
                    "next_offset": {"type": "integer", "nullable": true, "description": "The offset to request next, or null when has_more is false."},
                    "source_backend": {"type": "string", "description": "Backend that served the investigation, per handleImportDependencyInvestigation."},
                    "coverage": {"type": "object", "additionalProperties": true, "description": "Bounds and completeness. For file_import_cycles it carries cycle_max_length, cycle_enumeration_cap, cycle_enumeration_truncated, cycle_enumeration_stop_reason (none, cycle_cap, or step_budget), and cycle_enumeration_step_budget. It also carries cycle_edge_flags, an object with edges_considered, type_only_excluded, deferred_excluded, inferred, and flags_unknown: the deduplicated edges counted by class before anchor filtering and paging, so a client can tell how much of a result rests on inferred edges or on edges written before the flags existed."}
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
                  "description": "Call graph metric rows emitted by handleCallGraphMetrics.",
                  "properties": {
                    "metric_type": {"type": "string", "description": "Metric type echoed by handleCallGraphMetrics."},
                    "scope": {"type": "object", "description": "Resolved read scope keyed by scope field names, per handleCallGraphMetrics.", "additionalProperties": true},
                    "functions": {"type": "array", "description": "Canonical call graph metric rows.", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer", "description": "Rows on this page counted by handleCallGraphMetrics."},
                    "limit": {"type": "integer", "description": "Page bound echoed by handleCallGraphMetrics."},
                    "offset": {"type": "integer", "description": "Page offset echoed by handleCallGraphMetrics."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, per handleCallGraphMetrics."},
                    "next_offset": {"type": "integer", "description": "Next offset to request when truncated is true, per handleCallGraphMetrics.", "nullable": true},
                    "source_backend": {"type": "string", "description": "Backend that served the metrics, per handleCallGraphMetrics."},
                    "coverage": {"type": "object", "description": "Bounds and completeness for the metric read, per handleCallGraphMetrics.", "additionalProperties": true}
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
        "description": "Finds ranked files and symbols for a broad natural-language code topic using bounded content-index probes. Eligible 16-term searches run probes in parallel over a shared read-only snapshot and assemble the ranked candidate page in PostgreSQL; other requests use the bounded serial path. Returns coverage, truncation, source handles, and exact next-call handles for source reads and relationship stories. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
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
                  "description": "Ranked topic evidence page emitted by codequery/topic.go handleTopicInvestigation.",
                  "properties": {
                    "topic": {"type": "string", "description": "Topic investigated, echoed by handleTopicInvestigation."},
                    "intent": {"type": "string", "description": "Caller intent echoed by handleTopicInvestigation."},
                    "scope": {"type": "object", "description": "Resolved investigation scope (codeTopicScope) keyed by scope field names, per handleTopicInvestigation.", "additionalProperties": true},
                    "searched_terms": {"type": "array", "description": "Search terms probed for the topic, per handleTopicInvestigation.", "items": {"type": "string"}},
                    "matched_files": {"type": "array", "description": "Ranked file matches (content-index rows) returned by handleTopicInvestigation.", "items": {"type": "object", "additionalProperties": true}},
                    "matched_symbols": {"type": "array", "description": "Ranked symbol matches (content-index rows) returned by handleTopicInvestigation.", "items": {"type": "object", "additionalProperties": true}},
                    "evidence_groups": {"type": "array", "description": "Grouped evidence assembled by handleTopicInvestigation.", "items": {"type": "object", "additionalProperties": true}},
                    "call_graph_handles": {"type": "array", "description": "Call-graph follow-up handles for the top row, per codeTopicCallGraphHandle.", "items": {"type": "object", "additionalProperties": true}},
                    "recommended_next_calls": {"type": "array", "description": "Follow-up calls from codeTopicRecommendedNextCalls: source reads for the top row, or find_symbol plus search_file_content probes when no rows matched.", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer", "description": "Rows on this page emitted by handleTopicInvestigation."},
                    "limit": {"type": "integer", "description": "Page bound echoed by handleTopicInvestigation."},
                    "offset": {"type": "integer", "description": "Page offset echoed by handleTopicInvestigation."},
                    "truncated": {"type": "boolean", "description": "True when more ranked rows exist past this page, per handleTopicInvestigation."},
                    "candidate_pool_truncated": {"type": "boolean", "description": "True when the bounded per-term candidate pool reached its cap for at least one search term (#7008); the corpus may have additional, lower-ranked matches that never entered scoring."},
                    "deadline_truncated": {"type": "boolean", "description": "True when the read exceeded its context deadline and the response is a bounded partial of the scored rows gathered so far (#7008); lower-ranked matches may be missing. Distinct from candidate_pool_truncated, which reports a capped pool on a read that ran to completion. Also present as coverage.deadline_truncated."},
                    "source_backend": {"type": "string", "description": "Backend that served the topic investigation, per handleTopicInvestigation."},
                    "coverage": {"type": "object", "description": "Bounds and completeness for the topic investigation, per handleTopicInvestigation.", "additionalProperties": true},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer companion (answer.AnswerMetadata) derived from this payload by answer.AttachAnswerMetadata via handleTopicInvestigation.",
                      "properties": {
                        "schema_version": {"type": "string", "description": "Answer metadata schema version emitted by answer.BuildAnswerMetadata."},
                        "evidence_handles": {"type": "array", "description": "Evidence handles carried by the topic payload, per answer.BuildAnswerMetadata.", "items": {"type": "object", "additionalProperties": true}},
                        "missing_evidence": {"type": "array", "description": "Evidence the topic payload lacks, per answer.BuildAnswerMetadata.", "items": {"type": "object", "additionalProperties": true}},
                        "limitations": {"type": "array", "description": "Bounded caveats for the topic answer, per answer.BuildAnswerMetadata.", "items": {"type": "object", "additionalProperties": true}},
                        "truncated": {"type": "boolean", "description": "True when the topic answer or its coverage is truncated, per answer.BuildAnswerMetadata."},
                        "coverage": {"type": "object", "description": "Topic answer coverage keyed by coverage field names, per answer.BuildAnswerMetadata.", "additionalProperties": true},
                        "partial_reasons": {"type": "array", "description": "Why the topic answer is partial, per answer.BuildAnswerMetadata.", "items": {"type": "string"}},
                        "recommended_next_calls": {"type": "array", "description": "Follow-up calls for the topic answer, per answer.BuildAnswerMetadata.", "items": {"type": "object", "additionalProperties": true}}
                      },
                      "required": ["schema_version", "evidence_handles", "missing_evidence", "limitations", "truncated", "coverage", "partial_reasons", "recommended_next_calls"]
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {
            "description": "Backend unavailable. A replica behind the writer checkpoint, or a reader connection acquisition or identity check that timed out, answers a transient backend_unavailable with a Retry-After hint and error.details.retry_after_seconds. A PostgreSQL reader build that is not qualified for shared-snapshot export (#7957) refuses every search that would run on one shared snapshot (16 search terms on a reader with at least four connections): it answers a permanent backend_unavailable with the fixed message 'shared-snapshot read refused: the PostgreSQL reader build is not qualified for snapshot export', error.details.reason=snapshot_export_unqualified, and no Retry-After. It persists until the operator upgrades the reader to PostgreSQL 18.7 or later or declares the exact build in ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS; retrying does not help.",
            "headers": {
              "Retry-After": {
                "description": "Seconds to wait before retrying. Present only on the transient reader or graph verdicts; absent from the permanent snapshot_export_unqualified refusal.",
                "schema": {"type": "integer", "minimum": 1}
              }
            },
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
