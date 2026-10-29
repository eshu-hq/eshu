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
              "application/json": {"schema": {"type": "object"}}
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
