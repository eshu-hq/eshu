// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// RouteToCaller is the OpenAPI path fragment documenting the
// `/api/v0/code/routes/callers` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const RouteToCaller = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Exact route-to-caller trace, or unsupported when no HANDLES_ROUTE edge exists",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Exact route-to-caller trace emitted by handleRouteToCaller, or an unsupported verdict when no HANDLES_ROUTE edge exists.",
                  "properties": {
                    "status": {"type": "string", "enum": ["complete", "partial", "unsupported"], "description": "Trace outcome reported by handleRouteToCaller."},
                    "partial": {"type": "boolean", "description": "True when the traversal stopped early, per handleRouteToCaller."},
                    "truncated": {"type": "boolean", "description": "True when caller/callee rows were cut at the limit, per handleRouteToCaller."},
                    "unsupported": {"type": "array", "description": "Unsupported-route reasons reported by handleRouteToCaller.", "items": {"type": "string"}},
                    "truth_source": {"type": "string", "enum": ["HANDLES_ROUTE"], "description": "Edge family the trace was resolved from, per handleRouteToCaller."},
                    "route": {"type": "object", "description": "Resolved route endpoint keyed by endpoint field names, per handleRouteToCaller.", "additionalProperties": true},
                    "handler": {"type": "object", "description": "Resolved handler keyed by handler field names, per handleRouteToCaller; null when no exact handler exists.", "nullable": true, "additionalProperties": true},
                    "callers": {"type": "array", "description": "Bounded CALLS callers of the handler, per handleRouteToCaller.", "items": {"type": "object", "description": "One caller row (open map). Owner: handleRouteToCaller.", "additionalProperties": true}},
                    "callees": {"type": "array", "description": "Bounded CALLS callees of the handler, per handleRouteToCaller.", "items": {"type": "object", "additionalProperties": true, "description": "One callee row (open map). Owner: handleRouteToCaller."}},
                    "impact": {"type": "object", "description": "Materialized workload/repository impact summary keyed by impact field names, per handleRouteToCaller.", "additionalProperties": true},
                    "max_depth": {"type": "integer", "description": "Traversal depth bound echoed by handleRouteToCaller."},
                    "limit": {"type": "integer", "description": "Row bound echoed by handleRouteToCaller."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"description": "Ambiguous route selector; retry with a narrower repository, service, method, or path selector"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
