// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCodeDeadCodeScan = `
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
                  "candidate_kind": {"type": "string", "description": "Optional exact candidate kind. Restricts the bounded scan to the selected kind and rejects unsupported values instead of falling back to Function.", "enum": ["Function", "Class", "Struct", "Interface", "Trait", "SqlFunction"]},
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Dead code candidates",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "candidate_kind": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "language": {"type": "string"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean", "description": "True when either displayed results were clipped to limit or the bounded paged raw candidate scan reached its scan cap."},
                    "display_truncated": {"type": "boolean", "description": "True when filtered display results exceeded limit and were clipped."},
                    "candidate_scan_truncated": {"type": "boolean", "description": "True when the paged raw candidate scan reached candidate_scan_limit before exhausting candidates."},
                    "candidate_scan_limit": {"type": "integer", "description": "Maximum raw candidate rows the bounded dead-code scan may inspect across all selected candidate labels before policy exclusions."},
                    "candidate_scan_limit_per_label": {"type": "integer", "description": "Maximum share one candidate label may consume from the bounded scan's shared raw-row limit."},
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
                        "reflection_modeled": {
                          "type": "boolean",
                          "description": "True only when the requested language has modeled reflection reachability evidence."
                        },
                        "reflection_modeled_languages": {
                          "type": "array",
                          "items": {"type": "string"},
                          "description": "Languages whose reflection reachability evidence is modeled by the dead-code policy."
                        },
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
`
