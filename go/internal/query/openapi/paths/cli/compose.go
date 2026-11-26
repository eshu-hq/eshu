// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cli

// Compose is the OpenAPI path fragment documenting the
// POST /api/v0/service/report/compose route. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handler and
// docs/public/reference/http-api/cli-support-endpoints.md.
const Compose = `
    "/api/v0/service/report/compose": {
      "post": {
        "tags": ["cli"],
        "summary": "Compose a service intelligence report from captured inputs",
        "description": "Composes a service intelligence report from a captured service-story dossier, its truth envelope, and an optional captured supply-chain inventory with the same composer the offline command runs. All inputs are supplied; the server reads no backend. Oversize bodies are refused with 413.",
        "operationId": "composeServiceReport",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["dossier"],
                "properties": {
                  "dossier": {"type": "object"},
                  "truth": {"type": "object"},
                  "supply_chain": {"type": "object"},
                  "supply_chain_truth": {"type": "object"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Composed service intelligence report envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Service intelligence report (serviceintel.Report) composed from captured inputs by the composeReport handler. serviceintel.Compose is the emitting owner of every key.",
                  "properties": {
                    "schema": {"type": "string", "description": "Wire schema identifier (serviceintel.ReportSchema) emitted by the composeReport handler."},
                    "subject": {
                      "type": "object",
                      "description": "Identity anchor the report describes (serviceintel.ReportSubject) resolved by the composeReport handler.",
                      "properties": {
                        "service_name": {"type": "string", "description": "Canonical service name resolved by the composeReport handler."},
                        "service_id": {"type": "string", "description": "Canonical service identifier resolved by the composeReport handler, when known."},
                        "repo_id": {"type": "string", "description": "Owning repository identifier resolved by the composeReport handler, when known."},
                        "repo_name": {"type": "string", "description": "Owning repository name resolved by the composeReport handler, when known."}
                      },
                      "required": ["service_name"]
                    },
                    "supported": {"type": "boolean", "description": "False when the identity anchor resolved no service, per the composeReport handler."},
                    "partial": {"type": "boolean", "description": "True when any present section is partial or unsupported, per the composeReport handler."},
                    "truth_class": {"type": "string", "description": "Identity section truth class copied by the composeReport handler, never reclassified."},
                    "truth": {"type": "object", "description": "Copy of the identity section TruthEnvelope carried by the composeReport handler, null when the anchor is unsupported.", "additionalProperties": true},
                    "sections": {
                      "type": "array",
                      "description": "Fixed sections in canonical order (serviceintel.ReportSection) composed by the composeReport handler.",
                      "items": {
                        "type": "object",
                        "description": "One composed section: kind, title, status, and the canonical answer packet it was composed from.",
                        "properties": {
                          "kind": {"type": "string", "description": "Fixed section kind emitted by the composeReport handler."},
                          "title": {"type": "string", "description": "Human-readable section title emitted by the composeReport handler."},
                          "status": {"type": "string", "description": "Composed section status derived from the embedded answer packet by the composeReport handler."},
                          "answer": {"type": "object", "description": "Canonical answer packet (answer.AnswerPacket) the section was composed from; source of the section truth class, evidence, limitations, and next calls.",
                          "properties": {
                            "prompt_family": {"type": "string", "description": "Canonical prompt-family/capability identifier the packet answers, per answer.AnswerPacket."},
                            "question": {"type": "string", "description": "Canonical normalized question the packet answers, per answer.AnswerPacket."},
                            "primary_tool": {"type": "string", "description": "MCP tool or logical operation that produced the result, per answer.AnswerPacket."},
                            "primary_route": {"type": "string", "description": "HTTP route that produced the result, per answer.AnswerPacket."},
                            "truth_class": {"type": "string", "description": "Derived prompt-facing truth classification, per answer.AnswerPacket."},
                            "summary": {"type": "string", "description": "Human-readable answer, per answer.AnswerPacket."},
                            "supported": {"type": "boolean", "description": "False when required evidence was unavailable, per answer.AnswerPacket."},
                            "partial": {"type": "boolean", "description": "True when the answer is usable but incomplete, per answer.AnswerPacket."},
                            "result_ref": {"type": "string", "description": "Reference to the canonical envelope payload, per answer.AnswerPacket."},
                            "truth": {"type": "object", "description": "Copy of the envelope TruthEnvelope, per answer.AnswerPacket.", "additionalProperties": true},
                            "limitations": {"type": "array", "description": "Bounded human-readable caveats, per answer.AnswerPacket.", "items": {"type": "string"}},
                            "truncated": {"type": "boolean", "description": "Result-set truncation mirrored from the underlying query, per answer.AnswerPacket."},
                            "missing_evidence": {"type": "array", "description": "Evidence handles requested but not resolved, per answer.AnswerPacket.", "items": {"type": "object", "additionalProperties": true}},
                            "evidence_handles": {"type": "array", "description": "Addressable evidence handles behind the answer, per answer.AnswerPacket.", "items": {"type": "object", "additionalProperties": true}}
                          },
                          "required": ["truth_class", "supported", "partial"]
                        }
                        },
                        "required": ["kind", "title", "status", "answer"]
                      }
                    },
                    "limitations": {"type": "array", "description": "De-duplicated bounded caveats aggregated across sections by the composeReport handler.", "items": {"type": "string"}},
                    "recommended_next_calls": {
                      "type": "array",
                      "description": "De-duplicated bounded follow-up calls aggregated across sections by the composeReport handler, each traceable to a real tool, route, or playbook.",
                      "items": {
                        "type": "object",
                        "description": "One follow-up call (serviceintel.NextCall): a tool, route, or playbook step with bounded arguments.",
                        "properties": {
                          "tool": {"type": "string", "description": "MCP tool to call, per serviceintel.NextCall."},
                          "route": {"type": "string", "description": "HTTP route to call, per serviceintel.NextCall."},
                          "playbook": {"type": "string", "description": "Deterministic query playbook id, per serviceintel.NextCall."},
                          "reason": {"type": "string", "description": "Why the call is recommended, per serviceintel.NextCall."},
                          "arguments": {"type": "object", "additionalProperties": true, "description": "Bounded non-sensitive input values for the call; keys are the callee's argument names, per serviceintel.NextCall."}
                        }
                      }
                    },
                    "suggested_investigations": {
                      "type": "array",
                      "description": "Deterministic guided investigations derived from evidence gaps by the composeReport handler; empty when no section carries a supporting basis.",
                      "items": {"type": "object", "additionalProperties": true}
                    }
                  },
                  "required": ["schema", "subject", "supported", "partial", "truth_class", "sections"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "413": {
            "description": "Request body exceeds the 1 MiB compose cap (error envelope)"
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
