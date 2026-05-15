package query

const openAPIPathsCodeQuality = `
    "/api/v0/code/quality/inspect": {
      "post": {
        "tags": ["code"],
        "summary": "Inspect code quality metrics",
        "description": "Returns bounded function quality metrics for complexity, function length, argument count, or combined refactoring candidates.",
        "operationId": "inspectCodeQuality",
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
                  "min_complexity": {"type": "integer", "default": 10},
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
          "200": {
            "description": "Code quality inspection results",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "check": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "language": {"type": "string"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "thresholds": {"type": "object", "additionalProperties": true},
                    "results": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object", "additionalProperties": true}}
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
