// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cli

// Render is the OpenAPI path fragment documenting the
// POST /api/v0/investigation/packet/render route. openapi.Spec concatenates
// it into the published document; keep it in lockstep with the handler and
// docs/public/reference/http-api/cli-support-endpoints.md.
const Render = `
    "/api/v0/investigation/packet/render": {
      "post": {
        "tags": ["cli"],
        "summary": "Render an investigation evidence packet",
        "description": "Renders a client-supplied investigation evidence packet in the requested format (json, md, html) with the same local renderer the offline command runs. The packet is supplied in full; the server reads no backend. Oversize bodies are refused with 413.",
        "operationId": "renderInvestigationPacket",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["packet"],
                "properties": {
                  "packet": {"type": "object"},
                  "format": {"type": "string", "default": "json"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Rendered packet bytes (content type follows the format)",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "additionalProperties": true,
                  "description": "Rendered investigation evidence packet document (querycontract/packet.RenderInvestigationPacket JSON variant): the packet layers keyed by layer name, so a reader can audit every layer without parsing another format."
                }
              },
              "text/markdown": {"schema": {"type": "string", "description": "Deterministic markdown summary of the rendered packet, one fixed section per evidence layer, emitted by packet.RenderInvestigationPacket."}},
              "text/html": {"schema": {"type": "string", "description": "HTML rendering of the packet emitted by packet.RenderInvestigationPacket."}}
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "413": {
            "description": "Request body exceeds the 1 MiB render cap (error envelope)"
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
