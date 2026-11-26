// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dead

// Scan is the OpenAPI path fragment documenting the `/api/v0/code/dead-code`
// route. openapi.Spec concatenates it into the published document; keep it
// in lockstep with the handlers and docs/public/reference/http-api.md.
const Scan = `
    "/api/v0/code/dead-code": {
      "post": {
        "tags": ["code"],
        "summary": "Find dead code",
        "description": "Finds graph-backed dead-code candidates, applies the current default entrypoint/test/generated exclusions plus modeled language roots such as Go public-package exports, C parser-backed roots, C# parser-backed roots, Dart parser-backed roots, Haskell parser-backed roots, Kotlin parser-backed roots, Elixir parser-backed roots, Perl parser-backed roots, PHP parser-backed roots, and Groovy Jenkins roots, and classifies returned candidates without changing the derived truth envelope. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400. A candidate whose only incoming edges come from repositories outside a scoped token's grant is kept and marked ambiguous with the permission_hidden_consumer reason, never reported as unused and never silently dropped.",
        "operationId": "findDeadCode",
        "x-scoped-token-support": true,
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Dead code candidates",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "candidate_kind": {"type": "string", "description": "Candidate kind echoed by the dead-code scan handler."},
                    "repo_id": {"type": "string", "description": "Repository selector echoed by the dead-code scan handler."},
                    "language": {"type": "string", "description": "Parser language filter echoed by the dead-code scan handler."},
                    "limit": {"type": "integer", "description": "Candidate bound echoed by the dead-code scan handler."},
                    "truncated": {"type": "boolean", "description": "True when either displayed results were clipped to limit or the bounded paged raw candidate scan reached its scan cap."},
                    "display_truncated": {"type": "boolean", "description": "True when filtered display results exceeded limit and were clipped."},
                    "candidate_scan_truncated": {"type": "boolean", "description": "True when the paged raw candidate scan reached candidate_scan_limit before exhausting candidates."},
                    "candidate_scan_limit": {"type": "integer", "description": "Maximum raw candidate rows the bounded dead-code scan may inspect across all selected candidate labels before policy exclusions."},
                    "candidate_scan_limit_per_label": {"type": "integer", "description": "Maximum share one candidate label may consume from the bounded scan's shared raw-row limit."},
                    "candidate_scan_pages": {"type": "integer", "description": "Number of raw candidate pages read before returning results."},
                    "docstring_clip_bytes": {"type": "integer", "description": "Read-time docstring ceiling in bytes (512) applied to every row of this response; always present."},
                    "docstring_clipped_rows": {"type": "integer", "description": "Number of returned rows whose docstring was clipped; 0 when none."},
                    "candidate_scan_rows": {"type": "integer", "description": "Number of raw candidate rows inspected before policy exclusions."},
                    "results": {
                      "type": "array",
                      "description": "Dead-code candidates (content-index entity rows plus per-result classification) returned by the dead-code scan handler.",
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
                              },
                              "docstring_clipped": {"type": "boolean", "description": "Present and true only when this row's docstring was clipped at read time to docstring_clip_bytes. Every echo of the docstring (metadata, semantic_summary, semantic_profile, the language block, story) is derived from the clipped value; get_entity_content returns the stored docstring whole."},
                              "docstring_clip_bytes": {"type": "integer", "description": "Present only on a row whose docstring was clipped: the read-time docstring ceiling in bytes (512)."},
                              "docstring_total_bytes": {"type": "integer", "description": "Present only on a row whose docstring was clipped: the stored docstring length in bytes before the clip."}
                            }
                          }
                        ]
                      }
                    },
                    "analysis": {
                      "type": "object",
                      "description": "Policy analysis for the scan (deadcode policy stats and modeled roots) emitted by the dead-code scan handler.",
                      "properties": {
                        "root_categories_used": {"type": "array", "description": "Reachability root categories applied by the dead-code policy.", "items": {"type": "string"}},
                        "frameworks_recognized": {"type": "array", "description": "Frameworks the dead-code policy recognized on the candidates.", "items": {"type": "string"}},
                        "reflection_modeled": {
                          "type": "boolean",
                          "description": "True only when the requested language has modeled reflection reachability evidence."
                        },
                        "reflection_modeled_languages": {
                          "type": "array",
                          "items": {"type": "string"},
                          "description": "Languages whose reflection reachability evidence is modeled by the dead-code policy."
                        },
                        "tests_excluded": {"type": "boolean", "description": "Whether test files were excluded from the candidates by the dead-code policy."},
                        "generated_code_excluded": {"type": "boolean", "description": "Whether generated code was excluded from the candidates by the dead-code policy."},
                        "user_overrides_applied": {"type": "boolean", "description": "Whether user decorator exclusions were applied by the dead-code policy."},
                        "dead_code_language_maturity": {"type": "object", "description": "Dead-code language maturity by language, per the dead-code policy; keys are language names.", "additionalProperties": {"type": "string"}},
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
                        "frameworks_without_root_model": {
                          "type": "object",
                          "description": "Frameworks observed on returned candidates for which dead-code has no root model, grouped by language. Convention-dispatched entry points of those frameworks may be misreported as dead.",
                          "additionalProperties": {"type": "array", "items": {"type": "string"}}
                        },
                        "modeled_entrypoints": {"type": "array", "description": "Modeled entry points the dead-code policy treated as reachability roots.", "items": {"type": "string"}},
                        "modeled_public_api": {"type": "array", "description": "Modeled public API the dead-code policy treated as reachability roots.", "items": {"type": "string"}},
                        "notes": {"type": "array", "description": "Policy notes for the scan emitted by the dead-code scan handler.", "items": {"type": "string"}}
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
`
