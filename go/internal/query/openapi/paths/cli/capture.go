// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cli

// Capture is the OpenAPI path fragment documenting the
// POST /api/v0/report/bundle/capture route. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handler and
// docs/public/reference/http-api/cli-support-endpoints.md.
const Capture = `
    "/api/v0/report/bundle/capture": {
      "post": {
        "tags": ["cli"],
        "summary": "Capture a wrong-answer report bundle",
        "description": "Redacts, digests, and validates client-supplied capture inputs into a wrong_answer_report.v1 bundle with the same fail-closed gate the offline command runs. The envelope is supplied in full; the server issues no query. Credential-bearing targets are refused with 422 and never echoed. Oversize bodies are refused with 413.",
        "operationId": "captureReportBundle",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["surface", "target", "envelope"],
                "properties": {
                  "surface": {"type": "string"},
                  "target": {"type": "string"},
                  "method": {"type": "string"},
                  "params": {"type": "object", "additionalProperties": true, "description": "Query/body parameters as issued (including any parsed out of the target query string); every sensitive-named key is removed by the reportbundle redaction walk, so keys are the caller-issued parameter names. Owner: reportbundle.CaptureInput.Params."},
                  "profile": {"type": "string"},
                  "reporter_note": {"type": "string"},
                  "envelope": {"type": "object", "description": "Query response envelope the capture reads the answer from: data carries the rows, truth the envelope, error the failure. Owner: querycontract.ResponseEnvelope via reportbundle.CaptureInput.Envelope.",
                    "properties": {
                      "data": {"type": "object", "additionalProperties": true, "description": "Response data the capture redacts and digests; keys are the captured route's data keys. Owner: querycontract.ResponseEnvelope.Data."},
                      "truth": {"type": "object", "description": "Truth envelope returned by the captured query, stored verbatim by reportbundle.Capture. Owner: querycontract.TruthEnvelope.",
                        "properties": {
                          "level": {"type": "string", "description": "Truth level stored by reportbundle.Capture. Owner: querycontract.TruthEnvelope."},
                          "capability": {"type": "string", "description": "Capability the truth was resolved under, per reportbundle.Capture. Owner: querycontract.TruthEnvelope."},
                          "profile": {"type": "string", "description": "Query profile the truth was resolved under, per reportbundle.Capture. Owner: querycontract.TruthEnvelope."},
                          "basis": {"type": "string", "description": "Truth basis stored by reportbundle.Capture. Owner: querycontract.TruthEnvelope."},
                          "backend": {"type": "string", "description": "Graph backend stored by reportbundle.Capture, when set. Owner: querycontract.TruthEnvelope."},
                          "freshness": {"type": "object", "description": "Freshness state stored by reportbundle.Capture. Owner: querycontract.TruthFreshness.",
                            "properties": {
                              "state": {"type": "string", "description": "Freshness state stored by reportbundle.Capture. Owner: querycontract.TruthFreshness."},
                              "observed_at": {"type": "string", "description": "Freshness observation instant stored by reportbundle.Capture, when known. Owner: querycontract.TruthFreshness."},
                              "detail": {"type": "string", "description": "Freshness detail stored by reportbundle.Capture, when known. Owner: querycontract.TruthFreshness."},
                              "cause": {"type": "string", "description": "Closed freshness cause stored by reportbundle.Capture, when known. Owner: querycontract.TruthFreshness."},
                              "next_check": {"type": "object", "description": "Bounded follow-up call that drills into the cause, stored by reportbundle.Capture. Owner: querycontract.FreshnessNextCheck.",
                                "properties": {
                                  "tool": {"type": "string", "description": "MCP tool to call for the next check, per reportbundle.Capture. Owner: querycontract.FreshnessNextCheck."},
                                  "route": {"type": "string", "description": "HTTP route to call for the next check, per reportbundle.Capture. Owner: querycontract.FreshnessNextCheck."},
                                  "reason": {"type": "string", "description": "Why the next check resolves the cause, per reportbundle.Capture. Owner: querycontract.FreshnessNextCheck."},
                                  "params": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Bounded query parameters for the next check; keys are the callee's argument names. Owner: querycontract.FreshnessNextCheck.Params."}
                                }}
                            }},
                          "reason": {"type": "string", "description": "Human-readable truth reason stored by reportbundle.Capture, when set. Owner: querycontract.TruthEnvelope."},
                          "truncated": {"type": "boolean", "description": "Envelope-level truncation marker stored by reportbundle.Capture, when set. Owner: querycontract.TruthEnvelope."},
                          "omissions": {"type": "array", "description": "Sections withheld or reduced under the selection, per reportbundle.Capture. Owner: querycontract.TruthOmission.",
                            "items": {"type": "object", "description": "One withheld or reduced section, per reportbundle.Capture. Owner: querycontract.TruthOmission.",
                              "properties": {
                                "section": {"type": "string", "description": "Section withheld or reduced, per reportbundle.Capture. Owner: querycontract.TruthOmission."},
                                "detail": {"type": "string", "description": "How the section was reduced, per reportbundle.Capture. Owner: querycontract.TruthOmission."},
                                "total": {"type": "integer", "description": "Row count the route held for the section, per reportbundle.Capture. Owner: querycontract.TruthOmission."}
                              }}}
                        },
                        "required": ["level", "freshness"]},
                      "error": {"type": "object", "description": "Error envelope returned by the captured query when the call failed, stored verbatim by reportbundle.Capture. Owner: querycontract.ErrorEnvelope.",
                        "properties": {
                          "code": {"type": "string", "description": "Stable machine-readable error code stored by reportbundle.Capture. Owner: querycontract.ErrorEnvelope."},
                          "message": {"type": "string", "description": "Error message stored by reportbundle.Capture. Owner: querycontract.ErrorEnvelope."},
                          "capability": {"type": "string", "description": "Capability the error was raised under, per reportbundle.Capture, when set. Owner: querycontract.ErrorEnvelope."},
                          "correlation_id": {"type": "string", "description": "Correlation id stored by reportbundle.Capture, when set. Owner: querycontract.ErrorEnvelope."},
                          "profiles": {"type": "object", "description": "Active and required query profiles stored by reportbundle.Capture, when set. Owner: querycontract.ErrorProfiles.",
                            "properties": {
                              "current": {"type": "string", "description": "Query profile active when the error was raised. Owner: querycontract.ErrorProfiles."},
                              "required": {"type": "string", "description": "Minimum query profile the capability requires. Owner: querycontract.ErrorProfiles."}
                            }},
                          "details": {"type": "object", "additionalProperties": true, "description": "Error details keyed by detail name, per reportbundle.Capture. Owner: querycontract.ErrorEnvelope."}
                        },
                        "required": ["code", "message"]}
                    },
                    "required": ["data", "truth", "error"]},
                  "truncated": {"type": "boolean"},
                  "citations": {"type": "array", "description": "Share-safe citation references resolved for the captured response. Owner: reportbundle.CitationRef via reportbundle.CaptureInput.Citations.",
                    "items": {"type": "object", "description": "One share-safe citation reference: the evidence handle fields plus resolved citation_id, content_hash, and commit_sha; excerpt bytes never ride here. Owner: reportbundle.CitationRef.",
                      "properties": {
                        "kind": {"type": "string", "description": "Evidence handle kind stored by reportbundle.Capture. Owner: reportbundle.CitationRef."},
                        "repo_id": {"type": "string", "description": "Handle repository id stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "relative_path": {"type": "string", "description": "Handle relative path stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "entity_id": {"type": "string", "description": "Handle entity id stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "evidence_family": {"type": "string", "description": "Handle evidence family stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "reason": {"type": "string", "description": "Handle reason stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "start_line": {"type": "integer", "description": "Handle start line stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "end_line": {"type": "integer", "description": "Handle end line stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "confidence": {"type": "number", "description": "Handle confidence stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "citation_id": {"type": "string", "description": "Resolved citation id stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "content_hash": {"type": "string", "description": "Content hash stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "commit_sha": {"type": "string", "description": "Commit sha stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."}
                      }}},
                  "fact_refs": {"type": "array", "description": "Durable fact envelope references the caller hydrated, when known; never carry payload bytes. Owner: reportbundle.FactRef via reportbundle.CaptureInput.FactRefs.",
                    "items": {"type": "object", "description": "One durable fact envelope reference, mirroring the field names ifa.renderFact uses. Owner: reportbundle.FactRef.",
                      "properties": {
                        "fact_id": {"type": "string", "description": "Fact id supplied by the caller. Owner: reportbundle.FactRef."},
                        "stable_fact_key": {"type": "string", "description": "Stable fact key supplied by the caller. Owner: reportbundle.FactRef."},
                        "fact_kind": {"type": "string", "description": "Fact kind supplied by the caller. Owner: reportbundle.FactRef."},
                        "schema_version": {"type": "string", "description": "Fact schema version supplied by the caller, when known. Owner: reportbundle.FactRef."},
                        "scope_id": {"type": "string", "description": "Scope id supplied by the caller. Owner: reportbundle.FactRef."},
                        "generation_id": {"type": "string", "description": "Generation id supplied by the caller. Owner: reportbundle.FactRef."}
                      }}},
                  "fact_refs_reason": {"type": "string"},
                  "include_payloads": {"type": "boolean"},
                  "payload_excerpts": {"type": "array", "description": "Raw citation excerpts carried only under --include-payloads. Owner: reportbundle.CitationExcerpt via reportbundle.CaptureInput.PayloadExcerpts.",
                    "items": {"type": "object", "description": "One citation reference paired with its raw excerpt bytes; only ever inside the private-triage payload attachment. Owner: reportbundle.CitationExcerpt.",
                      "properties": {
                        "kind": {"type": "string", "description": "Evidence handle kind stored by reportbundle.Capture. Owner: reportbundle.CitationRef."},
                        "repo_id": {"type": "string", "description": "Handle repository id stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "relative_path": {"type": "string", "description": "Handle relative path stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "entity_id": {"type": "string", "description": "Handle entity id stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "evidence_family": {"type": "string", "description": "Handle evidence family stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "reason": {"type": "string", "description": "Handle reason stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "start_line": {"type": "integer", "description": "Handle start line stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "end_line": {"type": "integer", "description": "Handle end line stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "confidence": {"type": "number", "description": "Handle confidence stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "citation_id": {"type": "string", "description": "Resolved citation id stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "content_hash": {"type": "string", "description": "Content hash stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "commit_sha": {"type": "string", "description": "Commit sha stored by reportbundle.Capture, when known. Owner: reportbundle.CitationRef."},
                        "excerpt": {"type": "string", "description": "Raw excerpt bytes supplied by the caller; only valid under --include-payloads. Owner: reportbundle.CitationExcerpt."}
                      },
                      "required": ["excerpt"]}},
                  "payload_facts": {"type": "array", "description": "Raw fact envelopes carried only under --include-payloads. Owner: facts.Envelope via reportbundle.CaptureInput.PayloadFacts.",
                    "items": {"type": "object", "description": "One durable fact envelope; the struct carries no json tags so the wire keys are the Go field names. Owner: facts.Envelope.",
                      "properties": {
                        "FactID": {"type": "string", "description": "Fact id supplied by the caller. Owner: facts.Envelope."},
                        "ScopeID": {"type": "string", "description": "Scope id supplied by the caller. Owner: facts.Envelope."},
                        "GenerationID": {"type": "string", "description": "Generation id supplied by the caller. Owner: facts.Envelope."},
                        "FactKind": {"type": "string", "description": "Fact kind supplied by the caller. Owner: facts.Envelope."},
                        "StableFactKey": {"type": "string", "description": "Stable fact key supplied by the caller. Owner: facts.Envelope."},
                        "SchemaVersion": {"type": "string", "description": "Fact schema version supplied by the caller. Owner: facts.Envelope."},
                        "CollectorKind": {"type": "string", "description": "Collector kind supplied by the caller. Owner: facts.Envelope."},
                        "FencingToken": {"type": "integer", "description": "Fencing token supplied by the caller. Owner: facts.Envelope."},
                        "SourceConfidence": {"type": "string", "description": "Source confidence supplied by the caller. Owner: facts.Envelope."},
                        "ObservedAt": {"type": "string", "format": "date-time", "description": "Observation instant supplied by the caller. Owner: facts.Envelope."},
                        "Payload": {"type": "object", "additionalProperties": true, "description": "Fact payload keyed by payload field name, supplied by the caller. Owner: facts.Envelope."},
                        "IsTombstone": {"type": "boolean", "description": "Whether the envelope tombstones the fact, supplied by the caller. Owner: facts.Envelope."},
                        "SourceRef": {"type": "object", "description": "Source reference supplied by the caller; the struct carries no json tags so the wire keys are the Go field names. Owner: facts.Ref.",
                          "properties": {
                            "SourceSystem": {"type": "string", "description": "Source system supplied by the caller. Owner: facts.Ref."},
                            "ScopeID": {"type": "string", "description": "Scope id supplied by the caller. Owner: facts.Ref."},
                            "GenerationID": {"type": "string", "description": "Generation id supplied by the caller. Owner: facts.Ref."},
                            "FactKey": {"type": "string", "description": "Fact key supplied by the caller. Owner: facts.Ref."},
                            "SourceURI": {"type": "string", "description": "Source URI supplied by the caller. Owner: facts.Ref."},
                            "SourceRecordID": {"type": "string", "description": "Source record id supplied by the caller. Owner: facts.Ref."}
                          }}
                      }}}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Captured bundle envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Wrong-answer report bundle (reportbundle.Bundle) the server issues without querying a backend. reportbundle.Capture is the emitting owner of every key.",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Bundle wire schema version emitted by reportbundle.Capture."},
                    "bundle_id": {"type": "string", "description": "Bundle identity digest emitted by reportbundle.Capture."},
                    "created_at": {"type": "string", "format": "date-time", "description": "Capture instant emitted by reportbundle.Capture."},
                    "reporter_note": {"type": "string", "description": "Caller-supplied note echoed by reportbundle.Capture."},
                    "query": {
                      "type": "object",
                      "description": "What was asked (reportbundle.CapturedQuery): surface, target, and redacted parameters.",
                      "properties": {
                        "surface": {"type": "string", "description": "Query surface (api or mcp) recorded by reportbundle.Capture."},
                        "target": {"type": "string", "description": "Endpoint path or MCP tool name recorded by reportbundle.Capture."},
                        "method": {"type": "string", "description": "Request method recorded by reportbundle.Capture when supplied."},
                        "params": {"type": "object", "additionalProperties": true, "description": "Query/body parameters with every sensitive-named key removed by the reportbundle redaction walk; keys are the caller-issued parameter names."},
                        "profile": {"type": "string", "description": "Query profile in effect at capture time recorded by reportbundle.Capture, when known."}
                      },
                      "required": ["surface", "target", "params"]
                    },
                    "response": {
                      "type": "object",
                      "description": "What came back (reportbundle.CapturedResponse): verbatim truth envelope, redacted data, and the replay digest.",
                      "properties": {
                        "truth": {"type": "object", "description": "Verbatim querycontract.TruthEnvelope stored by reportbundle.Capture, never re-derived.",
                          "properties": {
                            "level": {"type": "string", "description": "Truth level stored by reportbundle.Capture."},
                            "capability": {"type": "string", "description": "Capability the truth was resolved under, per reportbundle.Capture."},
                            "profile": {"type": "string", "description": "Query profile the truth was resolved under, per reportbundle.Capture."},
                            "basis": {"type": "string", "description": "Truth basis stored by reportbundle.Capture."},
                            "backend": {"type": "string", "description": "Graph backend stored by reportbundle.Capture, when set."},
                            "freshness": {"type": "object", "description": "Freshness state stored by reportbundle.Capture.", "additionalProperties": true},
                            "reason": {"type": "string", "description": "Human-readable truth reason stored by reportbundle.Capture, when set."},
                            "truncated": {"type": "boolean", "description": "Envelope-level truncation marker stored by reportbundle.Capture, when set."},
                            "omissions": {"type": "array", "description": "Sections withheld or reduced under the caller's selection, per reportbundle.Capture.", "items": {"type": "object", "description": "One withheld or reduced section (open map). Owner: querycontract.TruthOmission.", "additionalProperties": true}}
                          },
                          "required": ["level", "freshness"]},
                        "error": {"type": "object", "description": "Verbatim querycontract.ErrorEnvelope stored by reportbundle.Capture when the captured call failed.",
                          "properties": {
                            "code": {"type": "string", "description": "Stable machine-readable error code stored by reportbundle.Capture."},
                            "message": {"type": "string", "description": "Error message stored by reportbundle.Capture."},
                            "capability": {"type": "string", "description": "Capability the error was raised under, per reportbundle.Capture, when set."},
                            "correlation_id": {"type": "string", "description": "Correlation id stored by reportbundle.Capture, when set."},
                            "details": {"type": "object", "description": "Error details keyed by detail name, per reportbundle.Capture.", "additionalProperties": true}
                          },
                          "required": ["code", "message"]},
                        "truncated": {"type": "boolean", "description": "Observed read-model truncation flag found in the captured data by reportbundle.Capture."},
                        "data": {"type": "object", "description": "Response data after the reportbundle key-name redaction walk; keys are the captured route's data keys.", "additionalProperties": true},
                        "data_digest": {"type": "string", "description": "Replay-equality anchor over the redacted data emitted by reportbundle.Capture."}
                      },
                      "required": ["truth", "truncated", "data", "data_digest"]
                    },
                    "evidence": {
                      "type": "object",
                      "description": "Citation and fact-reference trail (reportbundle.EvidenceContext); references only, never inline payload bytes.",
                      "properties": {
                        "citations": {"type": "array", "description": "Share-safe citation references (reportbundle.CitationRef) resolved for the captured response.", "items": {"type": "object", "description": "One share-safe citation reference. Owner: reportbundle.CitationRef.", "additionalProperties": true}},
                        "fact_refs": {"type": "array", "description": "Durable fact envelope references (reportbundle.FactRef); never carry payload bytes.", "items": {"type": "object", "description": "One durable fact envelope reference. Owner: reportbundle.FactRef.", "additionalProperties": true}},
                        "fact_refs_state": {"type": "string", "description": "resolved when fact refs were hydrated at capture time, unavailable when no public fact-record read let the CLI resolve them, per reportbundle.Capture."},
                        "fact_refs_reason": {"type": "string", "description": "Why fact refs are unavailable, per reportbundle.Capture."}
                      },
                      "required": ["fact_refs_state"]
                    },
                    "redaction": {
                      "type": "object",
                      "description": "Share-safe policy applied before serialization (reportbundle.RedactionProfile).",
                      "properties": {
                        "profile": {"type": "string", "description": "Redaction profile applied by reportbundle.Capture."},
                        "rules": {"type": "array", "description": "Sensitive key names the reportbundle redaction walk removed.", "items": {"type": "string"}}
                      },
                      "required": ["profile", "rules"]
                    },
                    "payloads": {
                      "type": "object",
                      "description": "Private-triage-only raw bytes (reportbundle.PayloadAttachment); present only when the capture used --include-payloads.",
                      "properties": {
                        "warning": {"type": "string", "description": "Self-describing private-triage warning emitted by reportbundle.Capture, always the first field."},
                        "excerpts": {"type": "array", "description": "Raw citation excerpts carried only under --include-payloads by reportbundle.Capture.", "items": {"type": "object", "description": "One raw citation excerpt. Owner: reportbundle.Capture.", "additionalProperties": true}},
                        "facts": {"type": "array", "description": "Raw fact envelopes carried only under --include-payloads by reportbundle.Capture.", "items": {"type": "object", "description": "One raw fact envelope. Owner: reportbundle.Capture.", "additionalProperties": true}}
                      },
                      "required": ["warning"]
                    },
                    "validation": {
                      "type": "object",
                      "description": "Deterministic checks reportbundle.Validate ran against the finished bundle (reportbundle.Validation).",
                      "properties": {
                        "status": {"type": "string", "description": "Validation outcome recorded by reportbundle.Validate."},
                        "checks": {"type": "array", "description": "Check names reportbundle.Validate ran.", "items": {"type": "string"}}
                      },
                      "required": ["status", "checks"]
                    }
                  },
                  "required": ["schema_version", "bundle_id", "created_at", "query", "response", "evidence", "redaction", "validation"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "413": {
            "description": "Request body exceeds the 8 MiB capture cap (error envelope)"
          },
          "422": {
            "description": "Capture refused the input (credential-bearing target, unredactable input, tripped share-safe gate; error envelope, secret never echoed)"
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
