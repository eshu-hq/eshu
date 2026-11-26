// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// InvestigationWorkflows is the OpenAPI path fragment documenting the
// `/api/v0/investigation-workflows`, `/api/v0/investigation-workflows/resolve`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const InvestigationWorkflows = `
    "/api/v0/investigation-workflows": {
      "get": {
        "tags": ["query"],
        "summary": "List guided investigation workflows",
        "description": "Returns the deterministic guided investigation workflow catalog. This is workflow-plan truth from static catalog data, not a live graph query. The default (compact) response returns id/name/version/domain/description per workflow with deterministic limit/offset paging (#6795); pass view=full for the complete shape (input shape, required/optional evidence, output packet, tool groups, starter prompts, and missing-evidence routing).",
        "operationId": "listInvestigationWorkflows",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 20}, "description": "Maximum number of workflows to return."},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Number of workflows to skip for paging."},
          {"name": "view", "in": "query", "required": false, "schema": {"type": "string", "enum": ["compact", "full"], "default": "compact"}, "description": "compact (default) returns id/name/version/domain/description; full returns the complete workflow shape."}
        ],
        "responses": {
          "200": {
            "description": "Guided investigation workflow catalog page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Catalog wire version, always investigation-workflows.v1. Owner: query/investigation_workflow_handler.go list."},
                    "workflows": {
                      "type": "array",
                      "description": "Workflow page in catalog order. Owner: query/investigation_workflow_handler.go list. Compact view (default) returns one InvestigationWorkflowSummary per row (id, name, version, domain, description); view=full returns the complete InvestigationWorkflow rows, which additionally carry required_inputs, required_evidence, optional_evidence, output_packet, tool_groups, starter_prompts, failure_modes, and missing_evidence_routes. Owner: query/investigation_workflow.go InvestigationWorkflowSummary and InvestigationWorkflow.",
                      "items": {
                        "description": "One investigation workflow: compact by default, full when view=full. Owner: query/investigation_workflow_handler.go list over query InvestigationWorkflowSummary and InvestigationWorkflow.",
                        "oneOf": [
                          {
                            "title": "compact",
                            "type": "object",
                            "description": "Compact workflow row returned by default (view omitted or compact). Owner: query/investigation_workflow.go InvestigationWorkflowSummary.",
                            "properties": {
                              "id": {"type": "string", "description": "Stable workflow id (guided_vulnerable_dependency, guided_deployable_drift, guided_incident_context). Owner: query/investigation_workflow.go InvestigationWorkflowSummary."},
                              "name": {"type": "string", "description": "Human-readable workflow name. Owner: query/investigation_workflow.go InvestigationWorkflowSummary."},
                              "version": {"type": "string", "description": "Workflow version. Owner: query/investigation_workflow.go InvestigationWorkflowSummary."},
                              "domain": {"type": "string", "description": "Guided domain (supply_chain, deployable_drift, incident_context). Owner: query/investigation_workflow.go InvestigationWorkflowSummary."},
                              "description": {"type": "string", "description": "What the workflow guides callers through. Owner: query/investigation_workflow.go InvestigationWorkflowSummary."}
                            }
                          },
                          {
                            "title": "full",
                            "type": "object",
                            "description": "Complete workflow row returned only when view=full. Owner: query/investigation_workflow.go InvestigationWorkflow.",
                            "properties": {
                              "id": {"type": "string", "description": "Stable workflow id. Owner: query/investigation_workflow.go InvestigationWorkflow."},
                              "name": {"type": "string", "description": "Human-readable workflow name. Owner: query/investigation_workflow.go InvestigationWorkflow."},
                              "version": {"type": "string", "description": "Workflow version. Owner: query/investigation_workflow.go InvestigationWorkflow."},
                              "domain": {"type": "string", "description": "Guided domain. Owner: query/investigation_workflow.go InvestigationWorkflow."},
                              "description": {"type": "string", "description": "What the workflow guides callers through. Owner: query/investigation_workflow.go InvestigationWorkflow."},
                              "required_inputs": {"type": "array", "description": "Declared caller inputs the resolver reads. Owner: query/investigation_workflow.go InvestigationWorkflow.RequiredInputs over query.PlaybookInput.", "items": {"type": "object", "description": "One declared workflow input. Owner: query/investigation_workflow.go PlaybookInput (playbook.Input).", "properties": {
                                "name": {"type": "string", "description": "Input key callers pass to Resolve. Owner: query/investigation_workflow.go PlaybookInput."},
                                "type": {"type": "string", "enum": ["string", "identifier"], "description": "Declared value kind. Owner: query/investigation_workflow.go PlaybookInput."},
                                "required": {"type": "boolean", "description": "Whether Resolve rejects the call when absent. Owner: query/investigation_workflow.go PlaybookInput."},
                                "description": {"type": "string", "description": "Input documentation for prompt-surface authors. Owner: query/investigation_workflow.go PlaybookInput; absent when empty."}
                              }}},
                              "required_evidence": {"type": "array", "description": "Evidence families a guided investigation expects. Owner: query/investigation_workflow.go InvestigationWorkflow.RequiredEvidence over WorkflowEvidence.", "items": {"type": "object", "description": "One expected evidence family. Owner: query/investigation_workflow.go WorkflowEvidence.", "properties": {
                                "key": {"type": "string", "description": "Evidence key. Owner: query/investigation_workflow.go WorkflowEvidence."},
                                "name": {"type": "string", "description": "Human-readable evidence name. Owner: query/investigation_workflow.go WorkflowEvidence."},
                                "description": {"type": "string", "description": "What the evidence covers. Owner: query/investigation_workflow.go WorkflowEvidence."},
                                "source_families": {"type": "array", "items": {"type": "string"}, "description": "Source families that can supply this evidence. Owner: query/investigation_workflow.go WorkflowEvidence; absent when empty."}
                              }}},
                              "optional_evidence": {"type": "array", "description": "Evidence families that enrich but never block resolution. Owner: query/investigation_workflow.go InvestigationWorkflow.OptionalEvidence over WorkflowEvidence.", "items": {"type": "object", "description": "One optional evidence family. Owner: query/investigation_workflow.go WorkflowEvidence.", "properties": {
                                "key": {"type": "string", "description": "Evidence key. Owner: query/investigation_workflow.go WorkflowEvidence."},
                                "name": {"type": "string", "description": "Human-readable evidence name. Owner: query/investigation_workflow.go WorkflowEvidence."},
                                "description": {"type": "string", "description": "What the evidence covers. Owner: query/investigation_workflow.go WorkflowEvidence."},
                                "source_families": {"type": "array", "items": {"type": "string"}, "description": "Source families that can supply this evidence. Owner: query/investigation_workflow.go WorkflowEvidence; absent when empty."}
                              }}},
                              "output_packet": {"type": "object", "description": "Bounded packet the workflow is expected to produce after the recommended calls run. Owner: query/investigation_workflow.go InvestigationWorkflow.OutputPacket over WorkflowOutputPacket.", "properties": {
                                "schema": {"type": "string", "description": "Output packet schema version. Owner: query/investigation_workflow.go WorkflowOutputPacket."},
                                "truth_labels": {"type": "array", "items": {"type": "string"}, "description": "Truth labels the packet may carry. Owner: query/investigation_workflow.go WorkflowOutputPacket."},
                                "sections": {"type": "array", "items": {"type": "string"}, "description": "Packet sections the workflow populates. Owner: query/investigation_workflow.go WorkflowOutputPacket."}
                              }},
                              "tool_groups": {"type": "array", "description": "High-level workflow groups over existing atomic tools. Owner: query/investigation_workflow.go InvestigationWorkflow.ToolGroups over WorkflowToolGroup.", "items": {"type": "object", "description": "One tool group. Owner: query/investigation_workflow.go WorkflowToolGroup.", "properties": {
                                "name": {"type": "string", "description": "Group name. Owner: query/investigation_workflow.go WorkflowToolGroup."},
                                "tools": {"type": "array", "items": {"type": "string"}, "description": "Atomic tool names in this group. Owner: query/investigation_workflow.go WorkflowToolGroup."}
                              }}},
                              "starter_prompts": {"type": "array", "items": {"type": "string"}, "description": "Starter prompts for the guided investigation. Owner: query/investigation_workflow.go InvestigationWorkflow.StarterPrompts."},
                              "failure_modes": {"type": "array", "description": "Expected missing, stale, hidden, or refused states and the bounded caller action. Owner: query/investigation_workflow.go InvestigationWorkflow.FailureModes over WorkflowFailureMode; absent when the workflow declares no failure modes.", "items": {"type": "object", "description": "One failure mode. Owner: query/investigation_workflow.go WorkflowFailureMode.", "properties": {
                                "condition": {"type": "string", "description": "Observed failure state. Owner: query/investigation_workflow.go WorkflowFailureMode."},
                                "meaning": {"type": "string", "description": "What the state means for truth. Owner: query/investigation_workflow.go WorkflowFailureMode."},
                                "recommended_action": {"type": "string", "description": "Bounded caller action that preserves truth. Owner: query/investigation_workflow.go WorkflowFailureMode."},
                                "related_evidence": {"type": "array", "items": {"type": "string"}, "description": "Related evidence keys. Owner: query/investigation_workflow.go WorkflowFailureMode; absent when empty."}
                              }}},
                              "missing_evidence_routes": {"type": "array", "description": "Observed missing-evidence keys mapped to the bounded calls that gather them. Owner: query/investigation_workflow.go InvestigationWorkflow.MissingEvidenceRoutes over WorkflowMissingEvidenceRoute.", "items": {"type": "object", "description": "One missing-evidence route. Owner: query/investigation_workflow.go WorkflowMissingEvidenceRoute.", "properties": {
                                "evidence_key": {"type": "string", "description": "Observed missing-evidence key this route answers. Owner: query/investigation_workflow.go WorkflowMissingEvidenceRoute."},
                                "states": {"type": "array", "items": {"type": "string"}, "description": "Evidence states this route covers. Owner: query/investigation_workflow.go WorkflowMissingEvidenceRoute; absent when empty."},
                                "calls": {"type": "array", "description": "Bounded calls that gather or explain the missing evidence. Owner: query/investigation_workflow.go WorkflowMissingEvidenceRoute over WorkflowNextCall.", "items": {"type": "object", "description": "One bounded follow-up call. Owner: query/investigation_workflow.go WorkflowNextCall.", "properties": {
                                  "id": {"type": "string", "description": "Call id within the missing-evidence route. Owner: query/investigation_workflow.go WorkflowNextCall."},
                                  "tool": {"type": "string", "description": "First-class tool to call. Owner: query/investigation_workflow.go WorkflowNextCall."},
                                  "reason": {"type": "string", "description": "Why this call gathers or explains the missing evidence. Owner: query/investigation_workflow.go WorkflowNextCall."},
                                  "missing_evidence": {"type": "string", "description": "Missing-evidence key this call was selected for. Owner: query/investigation_workflow.go WorkflowNextCall."},
                                  "params": {"type": "array", "description": "Bounded tool arguments. Owner: query/investigation_workflow.go WorkflowNextCall over PlaybookParam; absent when empty.", "items": {"type": "object", "description": "One bounded tool argument. Owner: query/investigation_workflow.go PlaybookParam (playbook.Param).", "properties": {
                                    "name": {"type": "string", "description": "Argument key passed to the tool. Owner: query/investigation_workflow.go PlaybookParam."},
                                    "from_input": {"type": "string", "description": "Declared input this argument binds to. Owner: query/investigation_workflow.go PlaybookParam; absent when the argument is a constant."},
                                    "const_string": {"type": "string", "description": "Constant string value. Owner: query/investigation_workflow.go PlaybookParam; absent unless the param sets a constant string."},
                                    "const_int": {"type": "integer", "description": "Constant integer value. Owner: query/investigation_workflow.go PlaybookParam; absent unless the param sets a constant integer."},
                                    "const_bool": {"type": "boolean", "description": "Constant boolean flag. Owner: query/investigation_workflow.go PlaybookParam; absent unless the param sets a constant boolean."}
                                  }}},
                                  "required_inputs_any": {"type": "array", "items": {"type": "string"}, "description": "Anchor inputs of which the caller must supply at least one. Owner: query/investigation_workflow.go WorkflowNextCall; absent when empty."},
                                  "expected_evidence": {"type": "string", "description": "Bounded evidence the call is expected to return. Owner: query/investigation_workflow.go WorkflowNextCall."}
                                }}}
                              }}}
                            }
                          }
                        ]
                      }
                    },
                    "versions": {
                      "type": "array",
                      "description": "Ordered (id, version) refs for the whole catalog. Owner: query/investigation_workflow.go InvestigationWorkflowCatalogVersions.",
                      "items": {
                          "type": "object",
                          "description": "One ordered (id, version) ref. Owner: query/investigation_workflow.go InvestigationWorkflowVersionRef.",
                          "properties": {
                            "id": {"type": "string", "description": "Stable workflow id. Owner: query/investigation_workflow.go InvestigationWorkflowVersionRef."},
                          "version": {"type": "string", "description": "Workflow version. Owner: query/investigation_workflow.go InvestigationWorkflowVersionRef."}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Workflows in this page. Owner: query/investigation_workflow_handler.go list."},
                    "total": {"type": "integer", "description": "Workflows in the catalog. Owner: query/investigation_workflow_handler.go list."},
                    "limit": {"type": "integer", "description": "Echo of the effective page bound. Owner: query/investigation_workflow_handler.go list."},
                    "offset": {"type": "integer", "description": "Echo of the requested page offset. Owner: query/investigation_workflow_handler.go list."},
                    "truncated": {"type": "boolean", "description": "True when more workflows remain past this page. Owner: query/investigation_workflow_handler.go pageInvestigationWorkflows."},
                    "next_offset": {"type": "integer", "nullable": true, "description": "offset to fetch the next page, or null when truncated is false. Owner: query/investigation_workflow_handler.go list via querycontract.NextOffset."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"}
        }
      }
    },
    "/api/v0/investigation-workflows/resolve": {
      "post": {
        "tags": ["query"],
        "summary": "Resolve a guided investigation workflow",
        "description": "Resolves one catalog workflow, declared inputs, and observed missing-evidence state into bounded recommended next calls. It does not execute the calls.",
        "operationId": "resolveInvestigationWorkflow",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["workflow_id"],
                "properties": {
                  "workflow_id": {"type": "string"},
                  "inputs": {"type": "object", "additionalProperties": {"type": "string"}},
                  "missing_evidence": {"type": "array", "items": {"type": "string"}}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Resolved recommended next calls",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Resolver wire version, always investigation-workflows.v1. Owner: query/investigation_workflow_handler.go resolve."},
                    "resolved": {
                      "type": "object",
                      "description": "Deterministic resolver output for the workflow and caller-supplied missing-evidence state. Owner: query/investigation_workflow_handler.go resolve with query ResolvedInvestigationWorkflow.",
                      "properties": {
                        "workflow_id": {"type": "string", "description": "Resolved workflow id. Owner: query/investigation_workflow.go ResolvedInvestigationWorkflow."},
                        "version": {"type": "string", "description": "Resolved workflow version. Owner: query/investigation_workflow.go ResolvedInvestigationWorkflow."},
                        "domain": {"type": "string", "description": "Resolved workflow domain. Owner: query/investigation_workflow.go ResolvedInvestigationWorkflow."},
                        "output_packet": {
                          "type": "object",
                          "description": "Bounded packet the workflow is expected to produce after the recommended calls run. Owner: query/investigation_workflow.go WorkflowOutputPacket.",
                          "properties": {
                            "schema": {"type": "string", "description": "Output packet schema version. Owner: query/investigation_workflow.go WorkflowOutputPacket."},
                            "truth_labels": {"type": "array", "items": {"type": "string"}, "description": "Truth labels the packet may carry. Owner: query/investigation_workflow.go WorkflowOutputPacket."},
                            "sections": {"type": "array", "items": {"type": "string"}, "description": "Packet sections the workflow populates. Owner: query/investigation_workflow.go WorkflowOutputPacket."}
                          }
                        },
                        "recommended_next_calls": {
                          "type": "array",
                          "description": "Concrete next calls for matched missing-evidence keys whose required anchor inputs were supplied. Owner: query/investigation_workflow.go InvestigationWorkflow Resolve.",
                          "items": {
                            "type": "object",
                            "description": "One resolved workflow call. Owner: query/investigation_workflow.go ResolvedWorkflowCall.",
                            "properties": {
                              "id": {"type": "string", "description": "Call id within the missing-evidence route. Owner: query/investigation_workflow.go ResolvedWorkflowCall."},
                              "tool": {"type": "string", "description": "First-class tool to call. Owner: query/investigation_workflow.go ResolvedWorkflowCall."},
                              "reason": {"type": "string", "description": "Why this call gathers or explains the missing evidence. Owner: query/investigation_workflow.go ResolvedWorkflowCall."},
                              "missing_evidence": {"type": "string", "description": "Missing-evidence key this call was selected for. Owner: query/investigation_workflow.go ResolvedWorkflowCall."},
                              "arguments": {"type": "object", "additionalProperties": true, "description": "Tool arguments resolved from the caller inputs; keys are the tool argument names bound from workflow inputs. Owner: query/investigation_workflow.go ResolvedWorkflowCall.Arguments."},
                              "expected_evidence": {"type": "string", "description": "Bounded evidence the call is expected to return. Owner: query/investigation_workflow.go ResolvedWorkflowCall."}
                            }
                          }
                        },
                        "blocked_next_calls": {
                          "type": "array",
                          "description": "Present only when matched calls were blocked: next calls whose required anchor inputs the caller did not supply. Owner: query/investigation_workflow.go InvestigationWorkflow Resolve.",
                          "items": {
                            "type": "object",
                            "description": "One blocked workflow call. Owner: query/investigation_workflow.go BlockedWorkflowCall.",
                            "properties": {
                              "id": {"type": "string", "description": "Call id within the missing-evidence route. Owner: query/investigation_workflow.go BlockedWorkflowCall."},
                              "tool": {"type": "string", "description": "First-class tool that was not recommended. Owner: query/investigation_workflow.go BlockedWorkflowCall."},
                              "reason": {"type": "string", "description": "Why this call would have gathered the missing evidence. Owner: query/investigation_workflow.go BlockedWorkflowCall."},
                              "missing_evidence": {"type": "string", "description": "Missing-evidence key this call was selected for. Owner: query/investigation_workflow.go BlockedWorkflowCall."},
                              "required_inputs_any": {"type": "array", "items": {"type": "string"}, "description": "Anchor inputs of which the caller supplied none. Owner: query/investigation_workflow.go BlockedWorkflowCall."}
                            }
                          }
                        },
                        "unmatched_missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Present only when supplied missing-evidence keys matched no route of the workflow. Owner: query/investigation_workflow.go InvestigationWorkflow Resolve."}
                      }
                    }
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
