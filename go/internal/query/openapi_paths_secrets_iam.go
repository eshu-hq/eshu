// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// #nosec G101 -- OpenAPI spec JSON whose const name contains "Secrets"/"IAM"; the value is a static API schema definition, not a credential literal
const openAPIPathsSecretsIAM = `
    "/api/v0/secrets-iam/posture-summary": {
      "get": {
        "tags": ["secrets-iam"],
        "summary": "Summarize secrets/IAM posture for a scope",
        "description": "Returns a bounded, scope-anchored rollup of the secrets/IAM reducer read models as provenance-only grouped counts: identity trust chains by state, privilege posture observations by risk type and severity, secret access paths by state, and posture gaps by gap type, plus S3 external-principal grant posture counts (total, by grant outcome, by resolution mode, and public/cross-account/service-principal tallies) read from the canonical GRANTS_ACCESS_TO graph edges. The grant section is omitted on deployments without a graph reader. No fingerprints, principal identities, paths, or evidence are exposed.",
        "operationId": "countSecretsIAMPosture",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Reducer scope ID to summarize."}
        ],
        "responses": {
          "200": {
            "description": "Posture summary counts",
            "content": {"application/json": {"schema": {"type": "object", "properties": {
              "scope_id": {"type": "string"},
              "summary": {"type": "object", "properties": {
                "identity_trust_chains_by_state": {"type": "array", "items": {"type": "object", "properties": {"bucket": {"type": "string"}, "count": {"type": "integer"}}, "required": ["bucket", "count"]}},
                "privilege_observations_by_risk_type": {"type": "array", "items": {"type": "object", "properties": {"bucket": {"type": "string"}, "count": {"type": "integer"}}, "required": ["bucket", "count"]}},
                "privilege_observations_by_severity": {"type": "array", "items": {"type": "object", "properties": {"bucket": {"type": "string"}, "count": {"type": "integer"}}, "required": ["bucket", "count"]}},
                "secret_access_paths_by_state": {"type": "array", "items": {"type": "object", "properties": {"bucket": {"type": "string"}, "count": {"type": "integer"}}, "required": ["bucket", "count"]}},
                "posture_gaps_by_gap_type": {"type": "array", "items": {"type": "object", "properties": {"bucket": {"type": "string"}, "count": {"type": "integer"}}, "required": ["bucket", "count"]}},
                "s3_external_principal_grant_posture": {"type": "object", "description": "S3 external-principal grant posture counts read from the canonical GRANTS_ACCESS_TO graph edges. Omitted on deployments without a graph reader.", "properties": {
                  "total_grants": {"type": "integer"},
                  "grants_by_outcome": {"type": "array", "items": {"type": "object", "properties": {"bucket": {"type": "string"}, "count": {"type": "integer"}}, "required": ["bucket", "count"]}},
                  "grants_by_resolution_mode": {"type": "array", "items": {"type": "object", "properties": {"bucket": {"type": "string"}, "count": {"type": "integer"}}, "required": ["bucket", "count"]}},
                  "public_grants": {"type": "integer"},
                  "cross_account_grants": {"type": "integer"},
                  "service_principal_grants": {"type": "integer"}
                }, "required": ["total_grants", "grants_by_outcome", "grants_by_resolution_mode", "public_grants", "cross_account_grants", "service_principal_grants"]}
              }}
            }, "required": ["scope_id", "summary"]}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/secrets-iam/identity-trust-chains": {
      "get": {
        "tags": ["secrets-iam"],
        "summary": "List secrets/IAM identity trust chains",
        "description": "Lists reducer-owned secrets/IAM identity trust chains (workload to ServiceAccount to IAM role to Vault policy). A chain is exact only when every hop resolves with explicit evidence; otherwise it stays provenance-only as partial, unresolved, stale, permission_hidden, or unsupported and is never promoted to graph truth. Rows carry fingerprints and join keys only, never secret values, raw paths, or token claims.",
        "operationId": "listSecretsIAMIdentityTrustChains",
        "x-scoped-token-support": true,
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
    "/api/v0/secrets-iam/privilege-posture-observations": {
      "get": {
        "tags": ["secrets-iam"],
        "summary": "List secrets/IAM privilege posture observations",
        "description": "Lists reducer-owned risky broad or partial posture observations (for example a role with external trust and no sts:ExternalId). Provenance-only; the reducer never promotes these to an exact path. Subject identifiers are fingerprints.",
        "operationId": "listSecretsIAMPrivilegePostureObservations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "observation_id", "in": "query", "schema": {"type": "string"}, "description": "Observation ID to anchor lookup."},
          {"name": "risk_type", "in": "query", "schema": {"type": "string"}, "description": "Optional risk type filter."},
          {"name": "severity", "in": "query", "schema": {"type": "string"}, "description": "Optional severity filter."},
          {"name": "state", "in": "query", "schema": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"]}},
          {"name": "after_observation_id", "in": "query", "schema": {"type": "string"}, "description": "Observation ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Privilege posture observation rows",
            "content": {"application/json": {"schema": {"type": "object", "properties": {
              "privilege_posture_observations": {"type": "array", "items": {"type": "object", "properties": {
                "observation_id": {"type": "string"},
                "risk_type": {"type": "string"},
                "severity": {"type": "string"},
                "state": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"]},
                "confidence": {"type": "string"},
                "subject_fingerprint": {"type": "string"},
                "reason": {"type": "string"},
                "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
              }, "required": ["observation_id", "state"]}},
              "count": {"type": "integer"}, "limit": {"type": "integer"}, "truncated": {"type": "boolean"},
              "next_cursor": {"type": "object", "properties": {"after_observation_id": {"type": "string"}}, "required": ["after_observation_id"]}
            }, "required": ["privilege_posture_observations", "count", "limit", "truncated"]}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/secrets-iam/secret-access-paths": {
      "get": {
        "tags": ["secrets-iam"],
        "summary": "List secrets/IAM secret access paths",
        "description": "Lists reducer-owned Vault policy-to-KV metadata access paths reachable from an exact identity chain. Paths are fingerprints and capabilities only; no secret value is ever returned.",
        "operationId": "listSecretsIAMSecretAccessPaths",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "path_id", "in": "query", "schema": {"type": "string"}, "description": "Secret access path ID to anchor lookup."},
          {"name": "chain_id", "in": "query", "schema": {"type": "string"}, "description": "Parent identity trust-chain ID to anchor lookup."},
          {"name": "vault_mount_join_key", "in": "query", "schema": {"type": "string"}, "description": "Vault mount join-key fingerprint to anchor lookup."},
          {"name": "state", "in": "query", "schema": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"]}},
          {"name": "after_path_id", "in": "query", "schema": {"type": "string"}, "description": "Path ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Secret access path rows",
            "content": {"application/json": {"schema": {"type": "object", "properties": {
              "secret_access_paths": {"type": "array", "items": {"type": "object", "properties": {
                "path_id": {"type": "string"},
                "chain_id": {"type": "string"},
                "state": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"]},
                "confidence": {"type": "string"},
                "kv_path_fingerprint": {"type": "string"},
                "vault_mount_join_key": {"type": "string"},
                "vault_policy_join_key": {"type": "string"},
                "capabilities": {"type": "array", "items": {"type": "string"}},
                "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
              }, "required": ["path_id", "state"]}},
              "count": {"type": "integer"}, "limit": {"type": "integer"}, "truncated": {"type": "boolean"},
              "next_cursor": {"type": "object", "properties": {"after_path_id": {"type": "string"}}, "required": ["after_path_id"]}
            }, "required": ["secret_access_paths", "count", "limit", "truncated"]}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/secrets-iam/posture-gaps": {
      "get": {
        "tags": ["secrets-iam"],
        "summary": "List secrets/IAM posture gaps",
        "description": "Lists reducer-owned missing, stale, permission_hidden, or unsupported evidence that blocks exact trust-chain truth. Gaps are surfaced rather than silently dropped.",
        "operationId": "listSecretsIAMPostureGaps",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "gap_id", "in": "query", "schema": {"type": "string"}, "description": "Posture gap ID to anchor lookup."},
          {"name": "gap_type", "in": "query", "schema": {"type": "string"}, "description": "Optional gap type filter."},
          {"name": "service_account_join_key", "in": "query", "schema": {"type": "string"}, "description": "ServiceAccount join-key fingerprint to anchor lookup."},
          {"name": "state", "in": "query", "schema": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"]}},
          {"name": "after_gap_id", "in": "query", "schema": {"type": "string"}, "description": "Gap ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Posture gap rows",
            "content": {"application/json": {"schema": {"type": "object", "properties": {
              "posture_gaps": {"type": "array", "items": {"type": "object", "properties": {
                "gap_id": {"type": "string"},
                "gap_type": {"type": "string"},
                "state": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"]},
                "reason": {"type": "string"},
                "service_account_join_key": {"type": "string"},
                "evidence_fact_ids": {"type": "array", "items": {"type": "string"}},
                "missing_evidence": {"type": "array", "items": {"type": "string"}},
                "unsupported_layers": {"type": "array", "items": {"type": "string"}}
              }, "required": ["gap_id", "state"]}},
              "count": {"type": "integer"}, "limit": {"type": "integer"}, "truncated": {"type": "boolean"},
              "next_cursor": {"type": "object", "properties": {"after_gap_id": {"type": "string"}}, "required": ["after_gap_id"]}
            }, "required": ["posture_gaps", "count", "limit", "truncated"]}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
