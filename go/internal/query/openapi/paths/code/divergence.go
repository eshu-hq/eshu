// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// Divergence is the OpenAPI path fragment documenting the
// `/api/v0/code/divergence/findings`,
// `/api/v0/code/divergence/investigate`, and
// `/api/v0/code/divergence/report` routes. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Divergence = `
    "/api/v0/code/divergence/findings": {
      "post": {
        "tags": ["code"],
        "summary": "Find parallel implementations",
        "description": "Reports repo-scoped parallel_implementation findings: functions with identical token streams (exact), identical streams up to renaming (renamed), reducer-verified near-duplicate pairs (drifted), call-graph-qualified wrapper families (wrapper_bypass; see kind), or cohort members missing a call the cohort majority makes (convention_outlier; see kind), ranked members x tokens with reasons that sum to the score. Suppressions are counted per rule, never silent; truth level is derived. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "findCodeDivergence",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Canonical repository identifier; required and resolved against the caller's grant"},
                  "kind": {"type": "string", "enum": ["", "exact", "renamed", "drifted", "wrapper_bypass", "convention_outlier", "parallel_implementation.exact", "parallel_implementation.renamed", "parallel_implementation.drifted", "parallel_implementation.wrapper_bypass", "parallel_implementation.convention_outlier"], "default": "", "description": "Family; blank reads all five. wrapper_bypass nominates from wrapper-family exact groups and qualifies one target at a time over one-hop graph rows; its fingerprint carries the target entity id. convention_outlier sweeps same-role cohorts (interface implementers, router handlers, package siblings) over cohort-bounded graph rows; its fingerprint carries the cohort address plus the majority callee. The qualified parallel_implementation.* spellings select the same families."},
                  "limit": {"type": "integer", "default": 25, "maximum": 100, "description": "Caps nominating groups per kind, not emitted findings: one wrapper family can qualify several targets, so a page may carry more findings than this limit"},
                  "offset": {"type": "integer", "default": 0, "maximum": 10000},
                  "include_tests": {"type": "boolean", "default": false, "description": "Opt test-file copies back into the member set (exact, renamed, and convention_outlier families; test files suppress by default). Drifted pairs touching test files are dropped at write, so include_tests has no effect on drifted findings"}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Divergence findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Bounded divergence findings page emitted by handleDivergenceFindings.",
                  "properties": {
                    "repo_id": {"type": "string", "description": "Repository selector echoed by handleDivergenceFindings."},
                    "kind": {"type": "string", "description": "Requested divergence family echoed by handleDivergenceFindings; empty reads all five."},
                    "limit": {"type": "integer", "description": "Nominating-group cap echoed by handleDivergenceFindings."},
                    "offset": {"type": "integer", "description": "Page offset echoed by handleDivergenceFindings."},
                    "findings": {"type": "array", "description": "Assembled findings in final score order, per handleDivergenceFindings.", "items": {
                      "type": "object",
                      "description": "One divergence finding (codedivergence.ResultMap): member rows with token reasons, plus the outlier block on convention_outlier findings.",
                      "properties": {
                        "finding_id": {"type": "string", "description": "Finding identity emitted by codedivergence.ResultMap."},
                        "kind": {"type": "string", "description": "Divergence family emitted by codedivergence.ResultMap."},
                        "fingerprint": {"type": "string", "description": "Finding fingerprint emitted by codedivergence.ResultMap."},
                        "score": {"type": "integer", "description": "Finding score emitted by codedivergence.ResultMap."},
                        "members": {"type": "array", "description": "Member rows emitted by codedivergence.ResultMap.", "items": {
                          "type": "object",
                          "description": "One member row: entity identity, token count, and source/drilldown handles.",
                          "properties": {
                            "entity_id": {"type": "string", "description": "Member entity id, per codedivergence.ResultMap."},
                            "entity_name": {"type": "string", "description": "Member entity name, per codedivergence.ResultMap."},
                            "entity_type": {"type": "string", "description": "Member entity type, per codedivergence.ResultMap."},
                            "file_path": {"type": "string", "description": "Member file path, per codedivergence.ResultMap."},
                            "relative_path": {"type": "string", "description": "Member repo-relative path, per codedivergence.ResultMap."},
                            "repo_id": {"type": "string", "description": "Member repository id, per codedivergence.ResultMap."},
                            "language": {"type": "string", "description": "Member language, per codedivergence.ResultMap."},
                            "package": {"type": "string", "description": "Member package derived from its path, per codedivergence.ResultMap."},
                            "start_line": {"type": "integer", "description": "Member start line, per codedivergence.ResultMap."},
                            "end_line": {"type": "integer", "description": "Member end line, per codedivergence.ResultMap."},
                            "token_count": {"type": "integer", "description": "Member token count, per codedivergence.ResultMap."},
                            "source_handle": {"type": "object", "description": "Source and drilldown handles for the member, per codedivergence.ResultMap.", "additionalProperties": true}
                          },
                          "required": ["entity_id", "entity_name", "file_path", "relative_path", "repo_id", "start_line", "end_line", "token_count", "source_handle"]
                        }},
                        "reasons": {"type": "array", "description": "Token reasons that sum to the score, per codedivergence.ResultMap.", "items": {
                          "type": "object",
                          "description": "One score reason: code, sentence, and value.",
                          "properties": {
                            "code": {"type": "string", "description": "Reason code, per codedivergence.ResultMap."},
                            "sentence": {"type": "string", "description": "Human-readable reason, per codedivergence.ResultMap."},
                            "value": {"type": "integer", "description": "Score contribution, per codedivergence.ResultMap."}
                          },
                          "required": ["code", "sentence", "value"]
                        }},
                        "confidence": {"type": "number", "description": "Finding confidence, per codedivergence.ResultMap; absent when zero."},
                        "cohort": {"type": "object", "description": "Outlier cohort that produced a convention_outlier finding, per codedivergence.outlierResultBlock; absent on other kinds.", "additionalProperties": true},
                        "majority_callee": {"type": "object", "description": "Majority callee of the outlier cohort, per codedivergence.outlierResultBlock; absent on other kinds.", "additionalProperties": true},
                        "share": {"type": "number", "description": "Cohort share of the majority callee, per codedivergence.outlierResultBlock; absent on other kinds."},
                        "outliers": {"type": "array", "description": "Outlier entity ids, per codedivergence.outlierResultBlock; absent on other kinds.", "items": {"type": "string"}},
                        "mediated": {"type": "array", "description": "Mediated outlier resolutions, per codedivergence.outlierResultBlock; absent on other kinds.", "items": {"type": "object", "description": "One mediated outlier: outlier, mediator, and mediator name.",
                          "properties": {
                            "outlier_id": {"type": "string", "description": "Outlier entity id, per codedivergence.outlierResultBlock."},
                            "mediator_id": {"type": "string", "description": "Mediator entity id, per codedivergence.outlierResultBlock."},
                            "mediator_name": {"type": "string", "description": "Mediator name, per codedivergence.outlierResultBlock."}
                          },
                          "required": ["outlier_id", "mediator_id", "mediator_name"]
                        }}
                      },
                      "required": ["finding_id", "kind", "fingerprint", "score", "members", "reasons"]
                    }},
                    "count": {"type": "integer", "description": "Findings on this page counted by handleDivergenceFindings."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, per handleDivergenceFindings."},
                    "next_offset": {"type": "integer", "description": "Cursor advance past the pre-suppression stat window, per nextDivergenceOffset."},
                    "suppressions": {"type": "object", "description": "Suppression counts by rule, per handleDivergenceFindings; keys are suppression rule names.", "additionalProperties": {"type": "integer"}},
                    "source_backend": {"type": "string", "description": "Backend that served the findings (content store, plus graph for wrapper/outlier tracks), per handleDivergenceFindings."}
                  },
                  "required": ["repo_id", "findings", "count", "truncated", "suppressions", "source_backend"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
    "/api/v0/code/divergence/investigate": {
      "post": {
        "tags": ["code"],
        "summary": "Investigate one divergence finding",
        "description": "Drills into one finding addressed by kind and fingerprint: the same member shape as the findings report plus bounded follow-up calls with arguments filled in (call chain and file range per member). Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "investigateCodeDivergence",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id", "kind", "fingerprint"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Canonical repository identifier; required and resolved against the caller's grant"},
                  "kind": {"type": "string", "enum": ["exact", "renamed", "drifted", "wrapper_bypass", "convention_outlier", "parallel_implementation.exact", "parallel_implementation.renamed", "parallel_implementation.drifted", "parallel_implementation.wrapper_bypass", "parallel_implementation.convention_outlier"]},
                  "fingerprint": {"type": "string", "description": "Finding fingerprint from a findings report entry (for wrapper_bypass, the target entity id; for convention_outlier, the cohort address plus the majority callee)"},
                  "include_tests": {"type": "boolean", "default": false, "description": "Opt test-file copies back into the member set (exact, renamed, and convention_outlier families; test files suppress by default). Drifted pairs touching test files are dropped at write, so include_tests has no effect on drifted findings"}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"description": "Finding not found"},
          "200": {
            "description": "Finding investigation",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Divergence finding investigation emitted by handleDivergenceInvestigate.",
                  "properties": {
                    "repo_id": {"type": "string", "description": "Repository selector echoed by handleDivergenceInvestigate."},
                    "finding": {"type": "object", "description": "Investigated finding (codedivergence.ResultMap): member rows with token reasons, per handleDivergenceInvestigate.", "additionalProperties": true},
                    "next_steps": {"type": "array", "description": "Bounded follow-up calls with filled-in arguments, per codedivergence.InvestigateSteps.", "items": {
                      "type": "object",
                      "description": "One investigation step: tool, args, and purpose.",
                      "properties": {
                        "tool": {"type": "string", "description": "Tool to call next, per codedivergence.InvestigateSteps."},
                        "args": {"type": "object", "description": "Filled-in step arguments keyed by argument name, per codedivergence.InvestigateSteps.", "additionalProperties": true},
                        "purpose": {"type": "string", "description": "Why the step follows, per codedivergence.InvestigateSteps."}
                      },
                      "required": ["tool", "args", "purpose"]
                    }},
                    "truncated": {"type": "boolean", "description": "True when follow-up steps were cut at the bound, per handleDivergenceInvestigate."},
                    "suppressions": {"type": "object", "description": "Suppression counts by rule, per handleDivergenceInvestigate; keys are suppression rule names.", "additionalProperties": {"type": "integer"}},
                    "source_backend": {"type": "string", "description": "Backend that served the investigation, per handleDivergenceInvestigate."}
                  },
                  "required": ["repo_id", "finding", "next_steps", "truncated", "suppressions", "source_backend"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
    "/api/v0/code/divergence/report": {
      "post": {
        "tags": ["code"],
        "summary": "Divergence rollup report",
        "description": "One-call rollup over all five divergence families for one repository: counts of assembled post-suppression findings by kind, the total, and the top top_per_kind findings per kind in final score order, with per-rule suppression counts and per-kind truncation flags. Content families scan every nominated group up to a 500-group hydration cap; wrapper_bypass qualifies nominated families up to a 200-family graph cap; convention_outlier always runs the whole cohort sweep. A capped window marks its kind truncated so a quiet count is distinguishable from a complete one. A graph track the bounded read budget cuts short degrades to a counted wrapper_graph_timeout or outlier_graph_timeout suppression with its kind truncated instead of failing the call. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "reportCodeDivergence",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Canonical repository identifier; required and resolved against the caller's grant"},
                  "top_per_kind": {"type": "integer", "default": 3, "minimum": 0, "maximum": 10, "description": "Top findings carried per kind; 0 selects the default; counts still cover the whole scanned window"},
                  "include_tests": {"type": "boolean", "default": false, "description": "Opt test-file copies back into the member set (exact, renamed, and convention_outlier families; test files suppress by default). Drifted pairs touching test files are dropped at write, so include_tests has no effect on drifted findings"}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Divergence rollup report",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "One-call rollup over all five divergence families emitted by handleDivergenceReport.",
                  "properties": {
                    "repo_id": {"type": "string", "description": "Repository selector echoed by handleDivergenceReport."},
                    "counts": {"type": "object", "description": "Assembled post-suppression finding counts by divergence family, per handleDivergenceReport; keys are family names (exact, renamed, drifted, wrapper_bypass, convention_outlier).", "additionalProperties": {"type": "integer"}},
                    "total": {"type": "integer", "description": "Total assembled findings across families counted by handleDivergenceReport."},
                    "top": {"type": "object", "description": "Top findings per family in final score order, per handleDivergenceReport; keys are family names.", "additionalProperties": {"type": "array", "items": {"type": "object", "description": "One top finding (open map). Owner: handleDivergenceReport.", "additionalProperties": true}}},
                    "top_per_kind": {"type": "integer", "description": "Top-findings bound echoed by handleDivergenceReport."},
                    "truncated": {"type": "object", "description": "Per-family truncation flags marking capped windows, per handleDivergenceReport; keys are family names.", "additionalProperties": {"type": "boolean"}},
                    "suppressions": {"type": "object", "description": "Suppression counts by rule, per handleDivergenceReport; keys are suppression rule names.", "additionalProperties": {"type": "integer"}},
                    "source_backend": {"type": "string", "description": "Backend that served the report, per handleDivergenceReport."}
                  },
                  "required": ["repo_id", "counts", "total", "top", "top_per_kind", "truncated", "suppressions", "source_backend"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
`
