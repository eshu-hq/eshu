// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Playbooks is the OpenAPI path fragment documenting the
// `/api/v0/query-playbooks`, `/api/v0/query-playbooks/resolve` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Playbooks = `
    "/api/v0/query-playbooks": {
      "get": {
        "tags": ["query"],
        "summary": "List query playbooks",
        "description": "Returns the deterministic query playbook catalog. This is workflow-plan truth from static catalog data, not a live graph query. The default (compact) response returns id/name/version/prompt_family/description per playbook with deterministic limit/offset paging (#6795); pass view=full for the complete shape (required inputs, ordered steps, expected truth, evidence, and failure modes).",
        "operationId": "listQueryPlaybooks",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 20}, "description": "Maximum number of playbooks to return."},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Number of playbooks to skip for paging."},
          {"name": "view", "in": "query", "required": false, "schema": {"type": "string", "enum": ["compact", "full"], "default": "compact"}, "description": "compact (default) returns id/name/version/prompt_family/description; full returns the complete playbook shape."}
        ],
        "responses": {
          "200": {
            "description": "Query playbook catalog page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Catalog schema version emitted by playbook.Handler.list."},
                    "playbooks": {"type": "array", "description": "Playbook rows emitted by playbook.Handler.list: playbook.Summary in compact view (id/name/version/prompt_family/description), playbook.Definition in full view.", "items": {
                      "type": "object",
                      "description": "One playbook row. Owner: playbook.Handler.list.",
                      "properties": {
                        "id": {"type": "string", "description": "Playbook id emitted by playbook.Handler.list (playbook.Definition.ID)."},
                        "name": {"type": "string", "description": "Playbook name emitted by playbook.Handler.list (playbook.Definition.Name)."},
                        "version": {"type": "string", "description": "Playbook version emitted by playbook.Handler.list (playbook.Definition.Version)."},
                        "prompt_family": {"type": "string", "description": "Prompt family emitted by playbook.Handler.list (playbook.Definition.PromptFamily)."},
                        "description": {"type": "string", "description": "Playbook description emitted by playbook.Handler.list (playbook.Definition.Description)."},
                        "required_inputs": {"type": "array", "description": "Declared inputs emitted by playbook.Handler.list (playbook.Input). Full view only.", "items": {
                          "type": "object",
                          "description": "One declared input. Owner: playbook.Handler.list (playbook.Input).",
                          "properties": {
                            "name": {"type": "string", "description": "Input key emitted by playbook.Handler.list (playbook.Input.Name)."},
                            "type": {"type": "string", "description": "Declared value kind emitted by playbook.Handler.list (playbook.Input.Type)."},
                            "required": {"type": "boolean", "description": "Whether Resolve rejects the playbook when the input is absent, emitted by playbook.Handler.list (playbook.Input.Required)."},
                            "description": {"type": "string", "description": "Input description emitted by playbook.Handler.list (playbook.Input.Description)."}
                          }
                        }},
                        "steps": {"type": "array", "description": "Ordered bounded calls emitted by playbook.Handler.list (playbook.Step). Full view only.", "items": {
                          "type": "object",
                          "description": "One ordered call. Owner: playbook.Handler.list (playbook.Step).",
                          "properties": {
                            "id": {"type": "string", "description": "Step id emitted by playbook.Handler.list (playbook.Step.ID)."},
                            "tool": {"type": "string", "description": "First-class tool the step invokes, emitted by playbook.Handler.list (playbook.Step.Tool)."},
                            "params": {"type": "array", "description": "Bounded call arguments emitted by playbook.Handler.list (playbook.Param).", "items": {
                              "type": "object",
                              "description": "One call argument. Owner: playbook.Handler.list (playbook.Param).",
                              "properties": {
                                "name": {"type": "string", "description": "Argument key emitted by playbook.Handler.list (playbook.Param.Name)."},
                                "from_input": {"type": "string", "description": "Declared input the argument binds to, emitted by playbook.Handler.list (playbook.Param.FromInput)."},
                                "const_string": {"type": "string", "description": "Constant string value emitted by playbook.Handler.list (playbook.Param.ConstString)."},
                                "const_int": {"type": "integer", "description": "Constant integer value emitted by playbook.Handler.list (playbook.Param.ConstInt)."},
                                "const_bool": {"type": "boolean", "description": "Constant boolean value emitted by playbook.Handler.list (playbook.Param.ConstBool)."}
                              }
                            }},
                            "expected_truth": {"type": "string", "description": "Expected truth class emitted by playbook.Handler.list (playbook.Step.ExpectedTruth)."},
                            "evidence_expected": {"type": "string", "description": "Expected evidence emitted by playbook.Handler.list (playbook.Step.EvidenceExpected)."},
                            "drilldowns": {"type": "array", "description": "Optional follow-up calls emitted by playbook.Handler.list (playbook.Drilldown).", "items": {
                              "type": "object",
                              "description": "One follow-up call. Owner: playbook.Handler.list (playbook.Drilldown).",
                              "properties": {
                                "tool": {"type": "string", "description": "Drilldown tool emitted by playbook.Handler.list (playbook.Drilldown.Tool)."},
                                "reason": {"type": "string", "description": "Drilldown reason emitted by playbook.Handler.list (playbook.Drilldown.Reason)."}
                              }
                            }}
                          }
                        }},
                        "failure_modes": {"type": "array", "description": "Declared truth/error conditions and fallbacks emitted by playbook.Handler.list (playbook.FailureMode). Full view only.", "items": {
                          "type": "object",
                          "description": "One declared failure mode. Owner: playbook.Handler.list (playbook.FailureMode).",
                          "properties": {
                            "condition": {"type": "string", "description": "Truth/error condition emitted by playbook.Handler.list (playbook.FailureMode.Condition)."},
                            "meaning": {"type": "string", "description": "Condition meaning emitted by playbook.Handler.list (playbook.FailureMode.Meaning)."},
                            "fallback": {"type": "string", "description": "Recommended recovery emitted by playbook.Handler.list (playbook.FailureMode.Fallback)."}
                          }
                        }}
                      }
                    }},
                    "versions": {"type": "array", "description": "Catalog (id, version) pairs emitted by playbook.Handler.list (playbook.VersionRef).", "items": {
                      "type": "object",
                      "description": "One catalog version pair. Owner: playbook.Handler.list (playbook.VersionRef).",
                      "properties": {
                        "id": {"type": "string", "description": "Playbook id emitted by playbook.Handler.list (playbook.VersionRef.ID)."},
                        "version": {"type": "string", "description": "Playbook version emitted by playbook.Handler.list (playbook.VersionRef.Version)."}
                      }
                    }},
                    "count": {"type": "integer", "description": "Playbooks on this page, emitted by playbook.Handler.list."},
                    "total": {"type": "integer", "description": "Playbooks matching the catalog, emitted by playbook.Handler.list."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by playbook.Handler.list."},
                    "offset": {"type": "integer", "description": "Requested page offset echoed by playbook.Handler.list."},
                    "truncated": {"type": "boolean", "description": "Whether more playbooks exist past this page, emitted by playbook.Handler.list."},
                    "next_offset": {"type": "integer", "nullable": true, "description": "offset to fetch the next page, or null when truncated is false, emitted by playbook.Handler.list."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"}
        }
      }
    },
    "/api/v0/query-playbooks/resolve": {
      "post": {
        "tags": ["query"],
        "summary": "Resolve a query playbook",
        "description": "Resolves one catalog playbook and declared inputs into an ordered, bounded call sequence. It does not execute the calls.",
        "operationId": "resolveQueryPlaybook",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["playbook_id"],
                "properties": {
                  "playbook_id": {"type": "string", "description": "Playbook id to resolve, read by playbook.Handler.resolve (resolveRequest.PlaybookID)."},
                  "inputs": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Declared playbook inputs as string pairs, read by playbook.Handler.resolve (resolveRequest.Inputs); undeclared inputs are rejected."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Resolved bounded call sequence",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Resolver schema version emitted by playbook.Handler.resolve."},
                    "resolved": {"type": "object", "description": "Deterministic ordered call sequence emitted by playbook.Handler.resolve (playbook.ResolvedPlaybook).", "properties": {
                      "playbook_id": {"type": "string", "description": "Resolved playbook id emitted by playbook.Handler.resolve (playbook.ResolvedPlaybook.PlaybookID)."},
                      "version": {"type": "string", "description": "Resolved playbook version emitted by playbook.Handler.resolve (playbook.ResolvedPlaybook.Version)."},
                      "prompt_family": {"type": "string", "description": "Prompt family emitted by playbook.Handler.resolve (playbook.ResolvedPlaybook.PromptFamily)."},
                      "calls": {"type": "array", "description": "Ordered fully specified calls emitted by playbook.Handler.resolve (playbook.ResolvedCall).", "items": {"type": "object", "description": "One resolved call. Owner: playbook.Handler.resolve (playbook.ResolvedCall).",
                        "properties": {
                          "step_id": {"type": "string", "description": "Originating step id emitted by playbook.Handler.resolve (playbook.ResolvedCall.StepID)."},
                          "tool": {"type": "string", "description": "First-class tool to call, emitted by playbook.Handler.resolve (playbook.ResolvedCall.Tool)."},
                          "arguments": {"type": "object", "additionalProperties": {}, "description": "Concrete bounded call arguments keyed by argument name, emitted by playbook.Handler.resolve (playbook.ResolvedCall.Arguments)."},
                          "expected_truth": {"type": "string", "description": "Expected truth class emitted by playbook.Handler.resolve (playbook.ResolvedCall.ExpectedTruth)."},
                          "evidence_expected": {"type": "string", "description": "Expected evidence emitted by playbook.Handler.resolve (playbook.ResolvedCall.EvidenceExpected)."},
                          "drilldowns": {"type": "array", "description": "Optional follow-up calls emitted by playbook.Handler.resolve (playbook.Drilldown).", "items": {"type": "object", "description": "One follow-up call. Owner: playbook.Handler.resolve (playbook.Drilldown).",
                            "properties": {
                              "tool": {"type": "string", "description": "Drilldown tool emitted by playbook.Handler.resolve (playbook.Drilldown.Tool)."},
                              "reason": {"type": "string", "description": "Drilldown reason emitted by playbook.Handler.resolve (playbook.Drilldown.Reason)."}
                            }
                          }}
                        }
                      }},
                      "failure_modes": {"type": "array", "description": "Declared truth/error conditions and fallbacks carried forward, emitted by playbook.Handler.resolve (playbook.FailureMode).", "items": {
                        "type": "object",
                        "description": "One declared failure mode. Owner: playbook.Handler.resolve (playbook.FailureMode).",
                        "properties": {
                          "condition": {"type": "string", "description": "Truth/error condition emitted by playbook.Handler.resolve (playbook.FailureMode.Condition)."},
                          "meaning": {"type": "string", "description": "Condition meaning emitted by playbook.Handler.resolve (playbook.FailureMode.Meaning)."},
                          "fallback": {"type": "string", "description": "Recommended recovery emitted by playbook.Handler.resolve (playbook.FailureMode.Fallback)."}
                        }
                      }}
                    }}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"}
        }
      }
    },
`
