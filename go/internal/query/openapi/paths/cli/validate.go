// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cli

// Validate is the OpenAPI path fragment documenting the
// POST /api/v0/report/bundle/validate route. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handler and
// docs/public/reference/http-api/cli-support-endpoints.md.
const Validate = `
    "/api/v0/report/bundle/validate": {
      "post": {
        "tags": ["cli"],
        "summary": "Validate a report bundle",
        "description": "Runs the share-safe validation gate over supplied bundle bytes. A 200 always carries the verdict: valid bundles answer valid true, share-unsafe bundles answer valid false with the reason. require_public mirrors the --require-public posture. Oversize bodies are refused with 413.",
        "operationId": "validateReportBundle",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "additionalProperties": false,
                "properties": {
                  "bundle": {"type": "object", "description": "Supplied report bundle bytes validated by the validateBundle handler against the share-safe gate (reportbundle.Bundle, the same wire shape the capture route issues). The key is optional: an absent bundle answers 200 valid:false with the reason, and only a malformed body or an unknown field answers 400.", "properties": {
                    "schema_version": {"type": "string", "description": "Bundle wire schema version the validateBundle handler checks against reportbundle.SchemaVersion."},
                    "bundle_id": {"type": "string", "description": "Bundle identity digest the validateBundle handler requires to be present."},
                    "created_at": {"type": "string", "format": "date-time", "description": "Capture instant carried by the supplied bundle."},
                    "reporter_note": {"type": "string", "description": "Caller-supplied note carried by the supplied bundle."},
                    "query": {"type": "object", "description": "What was asked (reportbundle.CapturedQuery): surface, target, and redacted parameters.",
                      "properties": {
                        "surface": {"type": "string", "description": "Query surface (api or mcp) carried by the supplied bundle."},
                        "target": {"type": "string", "description": "Endpoint path or MCP tool name carried by the supplied bundle."},
                        "method": {"type": "string", "description": "Request method carried by the supplied bundle when supplied."},
                        "params": {"type": "object", "additionalProperties": true, "description": "Query/body parameters with every sensitive-named key removed; keys are the caller-issued parameter names."},
                        "profile": {"type": "string", "description": "Query profile in effect at capture time carried by the supplied bundle, when known."}
                      },
                      "required": ["surface", "target", "params"]},
                    "response": {"type": "object", "description": "What came back (reportbundle.CapturedResponse): verbatim truth envelope, redacted data, and the replay digest.",
                      "properties": {
                        "truth": {"type": "object", "description": "Verbatim querycontract.TruthEnvelope carried by the supplied bundle, never re-derived.",
                          "properties": {
                            "level": {"type": "string", "description": "Truth level carried by the supplied bundle."},
                            "capability": {"type": "string", "description": "Capability the truth was resolved under, per the supplied bundle."},
                            "profile": {"type": "string", "description": "Query profile the truth was resolved under, per the supplied bundle."},
                            "basis": {"type": "string", "description": "Truth basis carried by the supplied bundle."},
                            "backend": {"type": "string", "description": "Graph backend carried by the supplied bundle, when set."},
                            "freshness": {"type": "object", "description": "Freshness state carried by the supplied bundle.", "additionalProperties": true},
                            "reason": {"type": "string", "description": "Human-readable truth reason carried by the supplied bundle, when set."},
                            "truncated": {"type": "boolean", "description": "Envelope-level truncation marker carried by the supplied bundle, when set."},
                            "omissions": {"type": "array", "description": "Sections withheld or reduced under the caller's selection, per the supplied bundle.", "items": {"type": "object", "additionalProperties": true}}
                          },
                          "required": ["level", "freshness"]},
                        "error": {"type": "object", "description": "Verbatim querycontract.ErrorEnvelope carried by the supplied bundle when the captured call failed.",
                          "properties": {
                            "code": {"type": "string", "description": "Stable machine-readable error code carried by the supplied bundle."},
                            "message": {"type": "string", "description": "Error message carried by the supplied bundle."},
                            "capability": {"type": "string", "description": "Capability the error was raised under, per the supplied bundle, when set."},
                            "correlation_id": {"type": "string", "description": "Correlation id carried by the supplied bundle, when set."},
                            "details": {"type": "object", "description": "Error details keyed by detail name, per the supplied bundle.", "additionalProperties": true}
                          },
                          "required": ["code", "message"]},
                        "truncated": {"type": "boolean", "description": "Observed read-model truncation flag found in the captured data."},
                        "data": {"type": "object", "description": "Response data after the reportbundle key-name redaction walk; keys are the captured route's data keys.", "additionalProperties": true},
                        "data_digest": {"type": "string", "description": "Replay-equality anchor over the redacted data carried by the supplied bundle."}
                      },
                      "required": ["truth", "truncated", "data", "data_digest"]},
                    "evidence": {"type": "object", "description": "Citation and fact-reference trail (reportbundle.EvidenceContext); references only, never inline payload bytes.",
                      "properties": {
                        "citations": {"type": "array", "description": "Share-safe citation references (reportbundle.CitationRef) carried by the supplied bundle.", "items": {"type": "object", "additionalProperties": true}},
                        "fact_refs": {"type": "array", "description": "Durable fact envelope references (reportbundle.FactRef) carried by the supplied bundle; never carry payload bytes.", "items": {"type": "object", "additionalProperties": true}},
                        "fact_refs_state": {"type": "string", "description": "resolved when fact refs were hydrated at capture time, unavailable when no public fact-record read let the CLI resolve them."},
                        "fact_refs_reason": {"type": "string", "description": "Why fact refs are unavailable, per the supplied bundle."}
                      },
                      "required": ["fact_refs_state"]},
                    "redaction": {"type": "object", "description": "Share-safe policy applied before serialization (reportbundle.RedactionProfile).",
                      "properties": {
                        "profile": {"type": "string", "description": "Redaction profile carried by the supplied bundle."},
                        "rules": {"type": "array", "description": "Sensitive key names the reportbundle redaction walk removed.", "items": {"type": "string"}}
                      },
                      "required": ["profile", "rules"]},
                    "payloads": {"type": "object", "description": "Private-triage-only raw bytes (reportbundle.PayloadAttachment); present only when the capture used --include-payloads.",
                      "properties": {
                        "warning": {"type": "string", "description": "Self-describing private-triage warning, always the first field."},
                        "excerpts": {"type": "array", "description": "Raw citation excerpts carried only under --include-payloads.", "items": {"type": "object", "additionalProperties": true}},
                        "facts": {"type": "array", "description": "Raw fact envelopes carried only under --include-payloads.", "items": {"type": "object", "additionalProperties": true}}
                      },
                      "required": ["warning"]},
                    "validation": {"type": "object", "description": "Deterministic checks reportbundle.Validate ran against the finished bundle (reportbundle.Validation).",
                      "properties": {
                        "status": {"type": "string", "description": "Validation outcome recorded by reportbundle.Validate."},
                        "checks": {"type": "array", "description": "Check names reportbundle.Validate ran.", "items": {"type": "string"}}
                      },
                      "required": ["status", "checks"]}
                  },
                  "required": ["schema_version", "bundle_id", "created_at", "query", "response", "evidence", "redaction", "validation"]},
                  "require_public": {"type": "boolean", "description": "Posture flag carried by the caller, mirroring --require-public: the validateBundle handler rejects a bundle whose redaction profile is not public when true."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Validation verdict envelope ({valid, message})",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Bundle validation verdict (cli/support.ValidateVerdict): a 200 always carries it, valid or not, because validation is a check, not a fetch.",
                  "properties": {
                    "valid": {"type": "boolean", "description": "Verdict emitted by the validateBundle handler: true when the bundle passes the share-safe gate."},
                    "message": {"type": "string", "description": "Refusal reason emitted by the validateBundle handler; empty when valid is true."}
                  },
                  "required": ["valid"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "413": {
            "description": "Request body exceeds the 1 MiB validate cap (error envelope)"
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
