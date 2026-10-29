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
                "required": ["bundle"],
                "properties": {
                  "bundle": {"type": "object"},
                  "require_public": {"type": "boolean"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Validation verdict envelope ({valid, message})",
            "content": {
              "application/json": {"schema": {"type": "object"}}
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
