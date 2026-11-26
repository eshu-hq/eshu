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
                        "type": "object",
                        "properties": {
                          "id": {"type": "string", "description": "Stable workflow id (guided_vulnerable_dependency, guided_deployable_drift, guided_incident_context). Owner: query/investigation_workflow.go InvestigationWorkflowSummary."},
                          "name": {"type": "string", "description": "Human-readable workflow name. Owner: query/investigation_workflow.go InvestigationWorkflowSummary."},
                          "version": {"type": "string", "description": "Workflow version. Owner: query/investigation_workflow.go InvestigationWorkflowSummary."},
                          "domain": {"type": "string", "description": "Guided domain (supply_chain, deployable_drift, incident_context). Owner: query/investigation_workflow.go InvestigationWorkflowSummary."},
                          "description": {"type": "string", "description": "What the workflow guides callers through. Owner: query/investigation_workflow.go InvestigationWorkflowSummary."}
                        }
                      }
                    },
                    "versions": {
                      "type": "array",
                      "description": "Ordered (id, version) refs for the whole catalog. Owner: query/investigation_workflow.go InvestigationWorkflowCatalogVersions.",
                      "items": {
                        "type": "object",
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
