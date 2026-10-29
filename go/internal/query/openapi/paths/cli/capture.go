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
              "application/json": {"schema": {"type": "object"}}
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
