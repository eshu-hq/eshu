// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Probes is the OpenAPI path fragment documenting the `/readyz` probe.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Probes = `
    "/readyz": {
      "get": {
        "tags": ["health"],
        "summary": "Readiness probe",
        "description": "Public and unauthenticated. Returns 200 only when both PostgreSQL and the pinned Neo4j backend are reachable; otherwise 503 with no Retry-After. The error field carries the probe cause.",
        "operationId": "getReadyz",
        "responses": {
          "200": {
            "description": "Ready: all backends reachable",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service": {"type": "string"},
                    "probe": {"type": "string", "example": "readyz"},
                    "status": {"type": "string", "example": "ok"}
                  }
                }
              }
            }
          },
          "503": {
            "description": "Not ready: a backend is unreachable or unconfigured; never carries Retry-After",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service": {"type": "string"},
                    "probe": {"type": "string", "example": "readyz"},
                    "status": {"type": "string", "example": "error"},
                    "error": {"type": "string"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
