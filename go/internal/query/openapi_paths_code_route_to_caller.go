// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCodeRouteToCaller = `
    "/api/v0/code/routes/callers": {
      "post": {
        "tags": ["code"],
        "summary": "Trace callers from an exact route handler",
        "description": "Resolves one route endpoint within an explicit repository or service/workload scope, requires an exact HANDLES_ROUTE edge before returning a handler, then traverses bounded CALLS evidence and summarizes materialized workload/repository impact evidence. Dynamic or unsupported routes are reported without guessing handlers.",
        "operationId": "traceRouteCallers",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["path"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Canonical repository identifier to scope route resolution"},
                  "service_id": {"type": "string", "description": "Optional exact service/workload identifier when repo_id is not supplied; resolved through Workload endpoint ownership"},
                  "service_name": {"type": "string", "description": "Optional exact service/workload name when repo_id is not supplied; resolved through Workload endpoint ownership"},
                  "method": {"type": "string", "description": "HTTP method matched exactly against HANDLES_ROUTE.http_method"},
                  "path": {"type": "string", "description": "Exact endpoint path as projected on the Endpoint node"},
                  "max_depth": {"type": "integer", "description": "Maximum CALLS traversal depth (default 2, max 5)", "default": 2, "minimum": 1, "maximum": 5},
                  "limit": {"type": "integer", "description": "Maximum combined caller/callee rows (default 25, max 100)", "default": 25, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Exact route-to-caller trace, or unsupported when no HANDLES_ROUTE edge exists",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string", "enum": ["complete", "partial", "unsupported"]},
                    "partial": {"type": "boolean"},
                    "truncated": {"type": "boolean"},
                    "unsupported": {"type": "array", "items": {"type": "string"}},
                    "truth_source": {"type": "string", "enum": ["HANDLES_ROUTE"]},
                    "route": {"type": "object", "additionalProperties": true},
                    "handler": {"type": "object", "nullable": true, "additionalProperties": true},
                    "callers": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "callees": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "impact": {"type": "object", "additionalProperties": true},
                    "max_depth": {"type": "integer"},
                    "limit": {"type": "integer"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"description": "Ambiguous route selector; retry with a narrower repository, service, method, or path selector"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/UnsupportedCapability"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
