// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// Quality is the OpenAPI path fragment documenting the
// `/api/v0/code/quality/inspect` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Quality = `
    "/api/v0/code/quality/inspect": {
      "post": {
        "tags": ["code"],
        "summary": "Inspect code quality metrics",
        "description": "Returns bounded function quality metrics for complexity, function length, argument count, or combined refactoring candidates. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "inspectCodeQuality",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "check": {"type": "string", "enum": ["complexity", "function_length", "argument_count", "refactoring_candidates"], "default": "refactoring_candidates"},
                  "repo_id": {"type": "string", "description": "Optional repository selector"},
                  "language": {"type": "string"},
                  "entity_id": {"type": "string"},
                  "function_name": {"type": "string"},
                  "min_complexity": {"type": "integer", "description": "Minimum cyclomatic complexity. When omitted or <= 0, the server uses 1 for complexity ranking and 10 for refactoring candidates."},
                  "min_lines": {"type": "integer", "default": 20},
                  "min_arguments": {"type": "integer", "default": 5},
                  "limit": {"type": "integer", "default": 10, "minimum": 1, "maximum": 100},
                  "offset": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Code quality inspection results",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Function quality inspection page emitted by handleCodeQualityInspection.",
                  "properties": {
                    "check": {"type": "string", "description": "Quality check echoed by handleCodeQualityInspection."},
                    "repo_id": {"type": "string", "description": "Repository selector echoed by handleCodeQualityInspection."},
                    "language": {"type": "string", "description": "Language filter echoed by handleCodeQualityInspection."},
                    "limit": {"type": "integer", "description": "Page bound echoed by handleCodeQualityInspection."},
                    "offset": {"type": "integer", "description": "Page offset echoed by handleCodeQualityInspection."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, per handleCodeQualityInspection."},
                    "thresholds": {"type": "object", "description": "Effective thresholds keyed by threshold name, per handleCodeQualityInspection.", "additionalProperties": true},
                    "results": {"type": "array", "description": "Quality rows (content-index rows plus per-row quality facts) returned by handleCodeQualityInspection.", "items": {"type": "object", "additionalProperties": true}},
                    "recommended_next_calls": {"type": "array", "description": "Follow-up get_file_lines calls from quality.NextCalls, one per result row.", "items": {"type": "object", "additionalProperties": true, "description": "One follow-up call (quality.NextCalls): tool get_file_lines with repo_id, relative_path, start_line, and end_line."}}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
