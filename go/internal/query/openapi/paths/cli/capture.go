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
                  "params": {"type": "object"},
                  "profile": {"type": "string"},
                  "reporter_note": {"type": "string"},
                  "envelope": {"type": "object"},
                  "truncated": {"type": "boolean"},
                  "citations": {"type": "array", "items": {"type": "object"}},
                  "fact_refs": {"type": "array", "items": {"type": "object"}},
                  "fact_refs_reason": {"type": "string"},
                  "include_payloads": {"type": "boolean"},
                  "payload_excerpts": {"type": "array", "items": {"type": "object"}},
                  "payload_facts": {"type": "array", "items": {"type": "object"}}
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
                            "omissions": {"type": "array", "description": "Sections withheld or reduced under the caller's selection, per reportbundle.Capture.", "items": {"type": "object", "additionalProperties": true}}
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
                        "citations": {"type": "array", "description": "Share-safe citation references (reportbundle.CitationRef) resolved for the captured response.", "items": {"type": "object", "additionalProperties": true}},
                        "fact_refs": {"type": "array", "description": "Durable fact envelope references (reportbundle.FactRef); never carry payload bytes.", "items": {"type": "object", "additionalProperties": true}},
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
                        "excerpts": {"type": "array", "description": "Raw citation excerpts carried only under --include-payloads by reportbundle.Capture.", "items": {"type": "object", "additionalProperties": true}},
                        "facts": {"type": "array", "description": "Raw fact envelopes carried only under --include-payloads by reportbundle.Capture.", "items": {"type": "object", "additionalProperties": true}}
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
