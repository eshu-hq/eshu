package query

const openAPIPathsSemanticStatus = `
    "/api/v0/status/semantic-extraction": {
      "get": {
        "tags": ["status"],
        "summary": "Get semantic extraction status",
        "description": "Returns optional semantic extraction capability status. No-provider mode is reported as unavailable without changing index, reducer, API, MCP, or documentation fact health.",
        "operationId": "getSemanticExtractionStatus",
        "responses": {
          "200": {
            "description": "Semantic extraction capability status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "state": {"type": "string", "enum": ["unavailable", "available", "available_but_disabled_for_scope", "disabled_by_policy", "provider_unhealthy"]},
                    "reason": {"type": "string"},
                    "provider_configured": {"type": "boolean"},
                    "documentation_observations_enabled": {"type": "boolean"},
                    "code_hints_enabled": {"type": "boolean"},
                    "deterministic_paths_affected": {"type": "boolean"},
                    "deterministic_documentation_unblocked": {"type": "boolean"},
                    "detail": {"type": "string"},
                    "updated_at": {"type": "string", "format": "date-time"},
                    "supported_states": {"type": "array", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
