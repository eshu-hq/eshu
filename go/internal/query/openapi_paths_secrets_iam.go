package query

const openAPIPathsSecretsIAM = `
    "/api/v0/secrets-iam/identity-trust-chains": {
      "get": {
        "tags": ["secrets-iam"],
        "summary": "List secrets/IAM identity trust chains",
        "description": "Lists reducer-owned secrets/IAM identity trust chains (workload to ServiceAccount to IAM role to Vault policy). A chain is exact only when every hop resolves with explicit evidence; otherwise it stays provenance-only as partial, unresolved, stale, permission_hidden, or unsupported and is never promoted to graph truth. Rows carry fingerprints and join keys only, never secret values, raw paths, or token claims.",
        "operationId": "listSecretsIAMIdentityTrustChains",
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "chain_id", "in": "query", "schema": {"type": "string"}, "description": "Identity trust-chain ID (the chain_id row identifier and next_cursor.after_chain_id) to anchor lookup."},
          {"name": "workload_object_id", "in": "query", "schema": {"type": "string"}, "description": "Durable workload object ID to anchor lookup."},
          {"name": "service_account_join_key", "in": "query", "schema": {"type": "string"}, "description": "ServiceAccount join-key fingerprint to anchor lookup."},
          {"name": "iam_role_fingerprint", "in": "query", "schema": {"type": "string"}, "description": "IAM role fingerprint to anchor lookup."},
          {"name": "state", "in": "query", "schema": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"]}},
          {"name": "after_chain_id", "in": "query", "schema": {"type": "string"}, "description": "Chain ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Identity trust-chain rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "identity_trust_chains": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "chain_id": {"type": "string"},
                          "state": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"]},
                          "confidence": {"type": "string"},
                          "service_account_join_key": {"type": "string"},
                          "workload_object_id": {"type": "string"},
                          "workload_kind": {"type": "string"},
                          "iam_role_fingerprint": {"type": "string"},
                          "vault_role_join_key": {"type": "string"},
                          "vault_mount_join_key": {"type": "string"},
                          "vault_policy_join_keys": {"type": "array", "items": {"type": "string"}},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}},
                          "source_scopes": {"type": "array", "items": {"type": "string"}},
                          "source_generations": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["chain_id", "state"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_chain_id": {"type": "string"}
                      },
                      "required": ["after_chain_id"]
                    }
                  },
                  "required": ["identity_trust_chains", "count", "limit", "truncated"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
