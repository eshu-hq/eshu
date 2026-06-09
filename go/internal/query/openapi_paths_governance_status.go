package query

const openAPIPathsGovernanceStatus = `
    "/api/v0/status/governance": {
      "get": {
        "tags": ["status"],
        "summary": "Get hosted governance status",
        "description": "Returns redacted hosted governance policy mode, state, readiness, and aggregate reason-code readbacks without raw policy bodies, subjects, tenants, source identifiers, credentials, provider endpoints, prompts, or provider responses.",
        "operationId": "getHostedGovernanceStatus",
        "responses": {
          "200": {
            "description": "Hosted governance status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "mode": {"type": "string", "enum": ["local_no_policy", "hosted_single_tenant", "hosted_multi_tenant"]},
                    "state": {"type": "string", "enum": ["disabled", "partial", "enforcing", "stale", "invalid"]},
                    "source_kind": {"type": "string", "enum": ["environment", "kubernetes_secret", "config_map", "postgres_revision", "unknown"]},
                    "policy_revision_hash": {"type": "string"},
                    "readiness": {"type": "object"},
                    "identity": {"type": "object"},
                    "tenancy": {"type": "object"},
                    "egress": {"type": "object"},
                    "semantic": {"type": "object"},
                    "extensions": {"type": "object"},
                    "redaction": {"type": "object"},
                    "retention": {"type": "object"},
                    "audit": {"type": "object"},
                    "aggregates": {"type": "object"},
                    "reasons": {"type": "array", "items": {"type": "string"}},
                    "supported_modes": {"type": "array", "items": {"type": "string"}},
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
