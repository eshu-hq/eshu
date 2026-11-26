// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

// IntelligenceReport is the OpenAPI path fragment documenting the
// `/api/v0/services/{service_name}/intelligence-report` route. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md.
const IntelligenceReport = `
    "/api/v0/services/{service_name}/intelligence-report": {
      "get": {
        "tags": ["entities"],
        "summary": "Service intelligence report",
        "description": "Composes the one-call service intelligence report: identity, code-to-runtime trace, deployment/config influence, supply-chain, and incident sections, each with preserved truth labels, evidence handles, limitations, and bounded next calls, plus deterministic suggested investigations. Returns schema service_intelligence_report.v1. Runs no LLM interpretation path. An ambiguous selector answers 409 with error.code ambiguous; each candidate in error.details.candidates carries next_selector, the argument (service_id) and value to pass next. A candidate with legacy_id true is a leftover name-only workload from the identity change, not the live workload; choose a scoped candidate.",
        "operationId": "getServiceIntelligenceReport",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {
            "name": "service_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional canonical workload id used to disambiguate the service selector"
          },
          {
            "name": "repo",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional repository selector. It separates only services in different repositories; two same-named services in one repository stay ambiguous and need service_id"
          },
          {
            "name": "environment",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional environment context"
          }
        ],
        "responses": {
          "200": {
            "description": "Composed service intelligence report",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema": {"type": "string", "description": "Report wire schema identifier emitted by getServiceIntelligenceReport (serviceintel.ReportSchema)."},
                    "subject": {"type": "object", "description": "Service the report describes, emitted by getServiceIntelligenceReport (serviceintel.ReportSubject).", "properties": {
                      "service_name": {"type": "string", "description": "Canonical service name emitted by getServiceIntelligenceReport (serviceintel.ReportSubject.ServiceName)."},
                      "service_id": {"type": "string", "description": "Canonical service id emitted by getServiceIntelligenceReport (serviceintel.ReportSubject.ServiceID); present when known."},
                      "repo_id": {"type": "string", "description": "Owning repository id emitted by getServiceIntelligenceReport (serviceintel.ReportSubject.RepoID); present when known."},
                      "repo_name": {"type": "string", "description": "Owning repository name emitted by getServiceIntelligenceReport (serviceintel.ReportSubject.RepoName); present when known."}
                    }},
                    "supported": {"type": "boolean", "description": "Whether the identity anchor resolved a service, emitted by getServiceIntelligenceReport (serviceintel.Report.Supported)."},
                    "partial": {"type": "boolean", "description": "Whether any present section is partial or unsupported, emitted by getServiceIntelligenceReport (serviceintel.Report.Partial)."},
                    "truth_class": {"type": "string", "description": "Identity section truth class, copied not reclassified, emitted by getServiceIntelligenceReport (serviceintel.Report.TruthClass)."},
                    "truth": {"type": "object", "description": "Copy of the identity section TruthEnvelope emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope); nil when the identity anchor is unsupported.", "properties": {
                      "level": {"type": "string", "description": "Truth level emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Level)."},
                      "capability": {"type": "string", "description": "Truth capability emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Capability)."},
                      "profile": {"type": "string", "description": "Query profile emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Profile)."},
                      "basis": {"type": "string", "description": "Truth basis emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Basis)."},
                      "backend": {"type": "string", "description": "Graph backend emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Backend)."},
                      "freshness": {"type": "object", "description": "Freshness metadata emitted by getServiceIntelligenceReport (querycontract.TruthFreshness).", "properties": {
                        "state": {"type": "string", "description": "Freshness state emitted by getServiceIntelligenceReport (querycontract.TruthFreshness.State)."},
                        "observed_at": {"type": "string", "description": "Observation time emitted by getServiceIntelligenceReport (querycontract.TruthFreshness.ObservedAt)."},
                        "detail": {"type": "string", "description": "Freshness detail emitted by getServiceIntelligenceReport (querycontract.TruthFreshness.Detail)."},
                        "cause": {"type": "string", "description": "Freshness cause emitted by getServiceIntelligenceReport (querycontract.TruthFreshness.Cause)."},
                        "next_check": {"type": "object", "description": "Bounded follow-up call that drills into the cause, emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck).", "properties": {
                          "tool": {"type": "string", "description": "MCP tool to call, emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck.Tool)."},
                          "route": {"type": "string", "description": "HTTP route to call, emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck.Route)."},
                          "reason": {"type": "string", "description": "What the next check resolves, emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck.Reason)."},
                          "params": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Bounded query parameters for the next check, emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck.Params)."}
                        }}
                      }},
                      "reason": {"type": "string", "description": "Truth reason emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Reason)."},
                      "truncated": {"type": "boolean", "description": "Whether the answer is a page, emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Truncated)."},
                      "omissions": {"type": "array", "description": "Response sections withheld or reduced, emitted by getServiceIntelligenceReport (querycontract.TruthOmission).", "items": {
                        "type": "object",
                        "description": "One withheld section. Owner: getServiceIntelligenceReport (querycontract.TruthOmission).",
                        "properties": {
                          "section": {"type": "string", "description": "Omitted section emitted by getServiceIntelligenceReport (querycontract.TruthOmission.Section)."},
                          "detail": {"type": "string", "description": "Omission detail emitted by getServiceIntelligenceReport (querycontract.TruthOmission.Detail)."},
                          "total": {"type": "integer", "description": "Row count held before the cut, emitted by getServiceIntelligenceReport (querycontract.TruthOmission.Total)."}
                        }
                      }}
                    }},
                    "sections": {"type": "array", "description": "Every fixed section in canonical order emitted by getServiceIntelligenceReport (serviceintel.ReportSection); unsupported sections stay visible with limitations and next calls.", "items": {
                      "type": "object",
                      "description": "One fixed report section. Owner: getServiceIntelligenceReport (serviceintel.ReportSection).",
                      "properties": {
                        "kind": {"type": "string", "enum": ["identity", "code_to_runtime", "deployment_config", "supply_chain", "incidents_support"], "description": "Fixed section kind emitted by getServiceIntelligenceReport (serviceintel.SectionKind)."},
                        "title": {"type": "string", "description": "Section title emitted by getServiceIntelligenceReport (serviceintel.ReportSection.Title)."},
                        "status": {"type": "string", "enum": ["supported", "partial", "unsupported"], "description": "Composed section status derived from the embedded answer packet, emitted by getServiceIntelligenceReport (serviceintel.SectionStatus)."},
                        "answer": {"type": "object", "description": "Canonical answer packet for the section emitted by getServiceIntelligenceReport (answer.AnswerPacket); it carries the section truth, evidence, limitations, and next calls.", "properties": {
                          "prompt_family": {"type": "string", "description": "Packet prompt family emitted by getServiceIntelligenceReport (answer.AnswerPacket.PromptFamily)."},
                          "question": {"type": "string", "description": "Canonical question emitted by getServiceIntelligenceReport (answer.AnswerPacket.Question)."},
                          "primary_tool": {"type": "string", "description": "Primary tool emitted by getServiceIntelligenceReport (answer.AnswerPacket.PrimaryTool)."},
                          "primary_route": {"type": "string", "description": "Primary route emitted by getServiceIntelligenceReport (answer.AnswerPacket.PrimaryRoute)."},
                          "truth_class": {"type": "string", "description": "Truth classification emitted by getServiceIntelligenceReport (answer.AnswerPacket.TruthClass)."},
                          "summary": {"type": "string", "description": "Human answer emitted by getServiceIntelligenceReport (answer.AnswerPacket.Summary); empty when unsupported or partial with no resolved evidence."},
                          "supported": {"type": "boolean", "description": "Whether required evidence was available, emitted by getServiceIntelligenceReport (answer.AnswerPacket.Supported)."},
                          "partial": {"type": "boolean", "description": "Whether the answer is usable but incomplete, emitted by getServiceIntelligenceReport (answer.AnswerPacket.Partial)."},
                          "result_ref": {"type": "string", "description": "Canonical envelope reference emitted by getServiceIntelligenceReport (answer.AnswerPacket.ResultRef)."},
                          "result": {"type": ["object", "null"], "additionalProperties": {}, "description": "Optional compact embedded copy of the envelope data emitted by getServiceIntelligenceReport via serviceintel.Compose (answer.AnswerPacket.Result); present only when the composer embeds the envelope copy (EmbedResult). The intelligence-report composer never embeds, so this key is currently absent and result_ref stays canonical."},
                          "truth": {"type": "object", "description": "Copy of the envelope TruthEnvelope emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope); nil only for unsupported answers built from an error.", "properties": {
                            "level": {"type": "string", "description": "Truth level emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Level)."},
                            "capability": {"type": "string", "description": "Truth capability emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Capability)."},
                            "profile": {"type": "string", "description": "Query profile emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Profile)."},
                            "basis": {"type": "string", "description": "Truth basis emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Basis)."},
                            "backend": {"type": "string", "description": "Graph backend emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Backend)."},
                            "freshness": {"type": "object", "description": "Freshness metadata emitted by getServiceIntelligenceReport (querycontract.TruthFreshness).", "properties": {
                              "state": {"type": "string", "description": "Freshness state emitted by getServiceIntelligenceReport (querycontract.TruthFreshness.State)."},
                              "observed_at": {"type": "string", "description": "Observation time emitted by getServiceIntelligenceReport (querycontract.TruthFreshness.ObservedAt)."},
                              "detail": {"type": "string", "description": "Freshness detail emitted by getServiceIntelligenceReport (querycontract.TruthFreshness.Detail)."},
                              "cause": {"type": "string", "description": "Freshness cause emitted by getServiceIntelligenceReport (querycontract.TruthFreshness.Cause)."},
                              "next_check": {"type": "object", "description": "Bounded follow-up call emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck).", "properties": {
                                "tool": {"type": "string", "description": "MCP tool to call, emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck.Tool)."},
                                "route": {"type": "string", "description": "HTTP route to call, emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck.Route)."},
                                "reason": {"type": "string", "description": "What the next check resolves, emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck.Reason)."},
                                "params": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Bounded query parameters emitted by getServiceIntelligenceReport (querycontract.FreshnessNextCheck.Params)."}
                              }}
                            }},
                            "reason": {"type": "string", "description": "Truth reason emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Reason)."},
                            "truncated": {"type": "boolean", "description": "Whether the answer is a page, emitted by getServiceIntelligenceReport (querycontract.TruthEnvelope.Truncated)."},
                            "omissions": {"type": "array", "description": "Withheld sections emitted by getServiceIntelligenceReport (querycontract.TruthOmission).", "items": {
                            "type": "object",
                            "description": "One withheld section. Owner: getServiceIntelligenceReport (querycontract.TruthOmission).",
                            "properties": {
                                "section": {"type": "string", "description": "Omitted section emitted by getServiceIntelligenceReport (querycontract.TruthOmission.Section)."},
                                "detail": {"type": "string", "description": "Omission detail emitted by getServiceIntelligenceReport (querycontract.TruthOmission.Detail)."},
                                "total": {"type": "integer", "description": "Row count held before the cut, emitted by getServiceIntelligenceReport (querycontract.TruthOmission.Total)."}
                              }
                            }}
                          }},
                          "limitations": {"type": "array", "items": {"type": "string"}, "description": "Human-readable caveats emitted by getServiceIntelligenceReport (answer.AnswerPacket.Limitations)."},
                          "truncated": {"type": "boolean", "description": "Underlying result-set truncation emitted by getServiceIntelligenceReport (answer.AnswerPacket.Truncated)."},
                          "missing_evidence": {"type": "array", "description": "Evidence handles requested but not resolved, emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle).", "items": {
                            "type": "object",
                            "description": "One missing evidence handle. Owner: getServiceIntelligenceReport (evidence.EvidenceCitationHandle).",
                            "properties": {
                              "kind": {"type": "string", "description": "Handle kind emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.Kind)."},
                              "repo_id": {"type": "string", "description": "Repository id emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.RepoID)."},
                              "relative_path": {"type": "string", "description": "Relative path emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.RelativePath)."},
                              "entity_id": {"type": "string", "description": "Entity id emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.EntityID)."},
                              "evidence_family": {"type": "string", "description": "Evidence family emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.EvidenceFamily)."},
                              "reason": {"type": "string", "description": "Handle reason emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.Reason)."},
                              "start_line": {"type": "integer", "description": "Start line emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.StartLine)."},
                              "end_line": {"type": "integer", "description": "End line emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.EndLine)."},
                              "confidence": {"type": "number", "description": "Handle confidence emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.Confidence)."}
                            }
                          }},
                          "evidence_handles": {"type": "array", "description": "Addressable evidence handles emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle).", "items": {
                            "type": "object",
                            "description": "One addressable evidence handle. Owner: getServiceIntelligenceReport (evidence.EvidenceCitationHandle).",
                            "properties": {
                              "kind": {"type": "string", "description": "Handle kind emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.Kind)."},
                              "repo_id": {"type": "string", "description": "Repository id emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.RepoID)."},
                              "relative_path": {"type": "string", "description": "Relative path emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.RelativePath)."},
                              "entity_id": {"type": "string", "description": "Entity id emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.EntityID)."},
                              "evidence_family": {"type": "string", "description": "Evidence family emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.EvidenceFamily)."},
                              "reason": {"type": "string", "description": "Handle reason emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.Reason)."},
                              "start_line": {"type": "integer", "description": "Start line emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.StartLine)."},
                              "end_line": {"type": "integer", "description": "End line emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.EndLine)."},
                              "confidence": {"type": "number", "description": "Handle confidence emitted by getServiceIntelligenceReport (evidence.EvidenceCitationHandle.Confidence)."}
                            }
                          }},
                          "citation_ref": {"type": "string", "description": "Citation packet reference emitted by getServiceIntelligenceReport (answer.AnswerPacket.CitationRef)."},
                          "recommended_next_calls": {"type": "array", "description": "Bounded follow-up calls in the evidence-citation shape, emitted by getServiceIntelligenceReport (answer.AnswerPacket.RecommendedNextCalls).", "items": {"type": "object", "description": "One follow-up call (open map). Owner: getServiceIntelligenceReport (answer.AnswerPacket.RecommendedNextCalls).", "additionalProperties": {}}},
                          "unsupported_reasons": {"type": "array", "items": {"type": "string"}, "description": "Why the answer is unsupported or partial, emitted by getServiceIntelligenceReport (answer.AnswerPacket.UnsupportedReasons)."}
                        }}
                      }
                    }},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "De-duplicated bounded caveats across sections emitted by getServiceIntelligenceReport (serviceintel.Report.Limitations)."},
                    "recommended_next_calls": {"type": "array", "description": "De-duplicated bounded follow-up calls emitted by getServiceIntelligenceReport (serviceintel.NextCall).", "items": {
                      "type": "object",
                      "description": "One follow-up call. Owner: getServiceIntelligenceReport (serviceintel.NextCall).",
                      "properties": {
                        "tool": {"type": "string", "description": "MCP tool to call, emitted by getServiceIntelligenceReport (serviceintel.NextCall.Tool)."},
                        "route": {"type": "string", "description": "HTTP route to call, emitted by getServiceIntelligenceReport (serviceintel.NextCall.Route)."},
                        "playbook": {"type": "string", "description": "Query playbook id emitted by getServiceIntelligenceReport (serviceintel.NextCall.Playbook)."},
                        "reason": {"type": "string", "description": "Why the call is recommended, emitted by getServiceIntelligenceReport (serviceintel.NextCall.Reason)."},
                        "arguments": {"type": "object", "additionalProperties": {}, "description": "Bounded non-sensitive input values emitted by getServiceIntelligenceReport (serviceintel.NextCall.Arguments)."}
                      }
                    }},
                    "suggested_investigations": {"type": "array", "description": "Deterministic guided investigations emitted by getServiceIntelligenceReport (serviceintel.SuggestedInvestigation).", "items": {
                      "type": "object",
                      "description": "One guided investigation. Owner: getServiceIntelligenceReport (serviceintel.SuggestedInvestigation).",
                      "properties": {
                        "id": {"type": "string", "description": "Stable suggestion id emitted by getServiceIntelligenceReport (serviceintel.SuggestedInvestigation.ID)."},
                        "section": {"type": "string", "description": "Source section emitted by getServiceIntelligenceReport (serviceintel.SuggestedInvestigation.Section)."},
                        "basis": {"type": "string", "description": "Grounding signal emitted by getServiceIntelligenceReport (serviceintel.SuggestedInvestigation.Basis)."},
                        "reason": {"type": "string", "description": "One-sentence reason emitted by getServiceIntelligenceReport (serviceintel.SuggestedInvestigation.Reason)."},
                        "evidence_basis": {"type": "array", "items": {"type": "string"}, "description": "Signal references emitted by getServiceIntelligenceReport (serviceintel.SuggestedInvestigation.EvidenceBasis)."},
                        "next_call": {"type": "object", "description": "Bounded executable follow-up emitted by getServiceIntelligenceReport (serviceintel.NextCall).", "properties": {
                          "tool": {"type": "string", "description": "MCP tool to call, emitted by getServiceIntelligenceReport (serviceintel.NextCall.Tool)."},
                          "route": {"type": "string", "description": "HTTP route to call, emitted by getServiceIntelligenceReport (serviceintel.NextCall.Route)."},
                          "playbook": {"type": "string", "description": "Query playbook id emitted by getServiceIntelligenceReport (serviceintel.NextCall.Playbook)."},
                          "reason": {"type": "string", "description": "Why the call is recommended, emitted by getServiceIntelligenceReport (serviceintel.NextCall.Reason)."},
                          "arguments": {"type": "object", "additionalProperties": {}, "description": "Bounded non-sensitive input values emitted by getServiceIntelligenceReport (serviceintel.NextCall.Arguments)."}
                        }},
                        "expected_truth_class": {"type": "string", "description": "Expected truth class emitted by getServiceIntelligenceReport (serviceintel.SuggestedInvestigation.ExpectedTruthClass)."}
                      }
                    }}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
`
