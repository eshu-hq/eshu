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
                    "provider_profiles": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "profile_id": {"type": "string"},
                          "display_name": {"type": "string"},
                          "provider_kind": {"type": "string", "enum": ["anthropic", "openai_compatible", "deepseek", "gemini", "bedrock", "azure_openai", "ollama", "internal_gateway"]},
                          "credential_source_kind": {"type": "string", "enum": ["kubernetes_secret", "vault_secret_handle", "environment_variable", "cloud_workload_identity", "local_dev_profile"]},
                          "credential_configured": {"type": "boolean"},
                          "model_id": {"type": "string"},
                          "endpoint_profile_id": {"type": "string"},
                          "source_classes": {"type": "array", "items": {"type": "string", "enum": ["documentation", "diagrams_images", "tickets_chat", "code_hints"]}},
                          "source_policy_configured": {"type": "boolean"},
                          "state": {"type": "string", "enum": ["configured", "unconfigured", "healthy", "unhealthy"]},
                          "reason": {"type": "string"},
                          "detail": {"type": "string"},
                          "updated_at": {"type": "string", "format": "date-time"}
                        }
                      }
                    },
                    "detail": {"type": "string"},
                    "updated_at": {"type": "string", "format": "date-time"},
                    "supported_states": {"type": "array", "items": {"type": "string"}},
                    "supported_provider_profile_states": {"type": "array", "items": {"type": "string"}}
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
