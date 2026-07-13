// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsHostedReadiness = `
    "/api/v0/status/hosted-readiness": {
      "get": {
        "tags": ["status"],
        "summary": "Get hosted operator readiness",
        "description": "Returns a fail-closed hosted readiness report across status snapshot loading, queue drain, collector completion, shared projection backlog, and API/MCP query readback.",
        "operationId": "getHostedReadiness",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "format",
            "in": "query",
            "required": false,
            "schema": {"type": "string", "enum": ["json", "text"], "default": "json"},
            "description": "Return a plain-text human summary when set to text."
          }
        ],
        "responses": {
          "200": {
            "description": "Hosted readiness report",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string"},
                    "state": {"type": "string", "enum": ["ready", "not_ready"]},
                    "ready": {"type": "boolean"},
                    "summary": {"type": "string"},
                    "generated_at": {"type": "string", "format": "date-time"},
                    "failure_classes": {"type": "array", "items": {"type": "string"}},
                    "repository_count": {"type": "integer"},
                    "queue": {"type": "object"},
                    "coordinator": {"type": "object"},
                    "checks": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "name": {"type": "string"},
                          "state": {"type": "string", "enum": ["pass", "fail"]},
                          "failure_class": {"type": "string"},
                          "detail": {"type": "string"},
                          "next_diagnostic": {"type": "string"}
                        }
                      }
                    },
                    "diagnostic_paths": {"type": "array", "items": {"type": "object"}}
                  }
                }
              },
              "text/plain": {
                "schema": {"type": "string"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
