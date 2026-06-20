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
                    "queue": {
                      "type": "object",
                      "description": "Audit-safe semantic extraction queue aggregates without source identifiers, prompts, provider responses, or credentials.",
                      "properties": {
                        "total": {"type": "integer"},
                        "pending": {"type": "integer"},
                        "claimed": {"type": "integer"},
                        "retrying": {"type": "integer"},
                        "succeeded": {"type": "integer"},
                        "dead_letter": {"type": "integer"},
                        "skipped": {"type": "integer"},
                        "no_provider": {"type": "integer"},
                        "policy_denied": {"type": "integer"},
                        "budget_exhausted": {"type": "integer"},
                        "unsafe": {"type": "integer"},
                        "provider_unavailable": {"type": "integer"},
                        "unchanged": {"type": "integer"},
                        "stale": {"type": "integer"},
                        "updated_at": {"type": "string", "format": "date-time"}
                      }
                    },
                    "budget": {
                      "type": "object",
                      "description": "Redacted semantic extraction token, cost, and budget decision aggregates.",
                      "properties": {
                        "estimated_input_tokens": {"type": "integer"},
                        "estimated_output_tokens": {"type": "integer"},
                        "estimated_cost_micros": {"type": "integer"},
                        "actual_input_tokens": {"type": "integer"},
                        "actual_output_tokens": {"type": "integer"},
                        "actual_cost_micros": {"type": "integer"},
                        "remaining_tokens": {"type": "integer"},
                        "remaining_cost_micros": {"type": "integer"},
                        "exhausted": {"type": "integer"}
                      }
                    },
                    "audit": {
                      "type": "object",
                      "description": "Class-based semantic extraction audit aggregates without principals, source identifiers, prompts, or provider responses.",
                      "properties": {
                        "actor_class_counts": {"type": "array", "items": {"type": "object"}},
                        "acl_state_counts": {"type": "array", "items": {"type": "object"}},
                        "last_processed_at": {"type": "string", "format": "date-time"}
                      }
                    },
                    "provider_profiles": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "profile_id": {"type": "string"},
                          "display_name": {"type": "string"},
                          "provider_kind": {"type": "string", "enum": ["anthropic", "openai_compatible", "deepseek", "minimax", "gemini", "bedrock", "azure_openai", "ollama", "internal_gateway"]},
                          "credential_source_kind": {"type": "string", "enum": ["kubernetes_secret", "vault_secret_handle", "environment_variable", "cloud_workload_identity", "local_dev_profile"]},
                          "credential_configured": {"type": "boolean"},
                          "model_id": {"type": "string"},
                          "endpoint_profile_id": {"type": "string"},
                          "source_classes": {"type": "array", "items": {"type": "string", "enum": ["documentation", "diagrams_images", "tickets_chat", "code_hints", "search_documents", "agent_reasoning"]}},
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
