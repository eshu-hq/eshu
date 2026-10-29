// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cli

// Score is the OpenAPI path fragment documenting the
// POST /api/v0/answer-quality/score route. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handler and
// docs/public/reference/http-api/cli-support-endpoints.md.
const Score = `
    "/api/v0/answer-quality/score": {
      "post": {
        "tags": ["cli"],
        "summary": "Score captured answer evidence",
        "description": "Scores redacted answer-quality evidence against the dogfood criteria with the same scorer the offline command runs. The evidence is supplied in full; the server reads no backend. Oversize bodies are refused with 413.",
        "operationId": "scoreAnswerQuality",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["version", "prompts"],
                "properties": {
                  "version": {"type": "string"},
                  "run_id": {"type": "string"},
                  "eshu_commit": {"type": "string"},
                  "prompts": {"type": "array", "items": {"type": "object"}}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Scorecard verdict envelope",
            "content": {
              "application/json": {"schema": {"type": "object"}}
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "413": {
            "description": "Request body exceeds the 1 MiB score cap (error envelope)"
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
