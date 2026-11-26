// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrastructure

// #nosec G101 -- OpenAPI spec JSON whose const name contains "Secrets"/"IAM"; the value is a static API schema definition, not a credential literal
const SecretsIAM = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Posture summary counts",
            "content": {"application/json": {"schema": {"type": "object", "properties": {
              "scope_id": {"type": "string", "description": "Echo of the caller's reducer scope, emitted by the secrets summary handler from the scope_id query param."},
              "summary": {"type": "object", "description": "Bounded scope-anchored rollup emitted by the secrets summary handler from secrets.IAMPostureSummary.", "properties": {
                "identity_trust_chains_by_state": {"type": "array", "description": "Trust-chain counts grouped by state, emitted by the secrets summary handler from IAMPostureSummary.IdentityTrustChainsByState.", "items": {"type": "object",
                            "description": "One bucket count row. Owner: secrets summary handler.", "properties": {"bucket": {"type": "string", "description": "State bucket emitted by secrets.IAMBucketCount.Bucket."}, "count": {"type": "integer", "description": "Rows in this bucket emitted by secrets.IAMBucketCount.Count."}}, "required": ["bucket", "count"]}},
                "privilege_observations_by_risk_type": {"type": "array", "description": "Privilege observation counts grouped by risk type, emitted by the secrets summary handler from IAMPostureSummary.PrivilegeObservationsByRiskType.", "items": {"type": "object",
                            "description": "One bucket count row. Owner: secrets summary handler.", "properties": {"bucket": {"type": "string", "description": "Risk-type bucket emitted by secrets.IAMBucketCount.Bucket."}, "count": {"type": "integer", "description": "Rows in this bucket emitted by secrets.IAMBucketCount.Count."}}, "required": ["bucket", "count"]}},
                "privilege_observations_by_severity": {"type": "array", "description": "Privilege observation counts grouped by severity, emitted by the secrets summary handler from IAMPostureSummary.PrivilegeObservationsBySeverity.", "items": {"type": "object",
                            "description": "One bucket count row. Owner: secrets summary handler.", "properties": {"bucket": {"type": "string", "description": "Severity bucket emitted by secrets.IAMBucketCount.Bucket."}, "count": {"type": "integer", "description": "Rows in this bucket emitted by secrets.IAMBucketCount.Count."}}, "required": ["bucket", "count"]}},
                "secret_access_paths_by_state": {"type": "array", "description": "Access-path counts grouped by state, emitted by the secrets summary handler from IAMPostureSummary.SecretAccessPathsByState.", "items": {"type": "object",
                            "description": "One bucket count row. Owner: secrets summary handler.", "properties": {"bucket": {"type": "string", "description": "State bucket emitted by secrets.IAMBucketCount.Bucket."}, "count": {"type": "integer", "description": "Rows in this bucket emitted by secrets.IAMBucketCount.Count."}}, "required": ["bucket", "count"]}},
                "posture_gaps_by_gap_type": {"type": "array", "description": "Posture-gap counts grouped by gap type, emitted by the secrets summary handler from IAMPostureSummary.PostureGapsByGapType.", "items": {"type": "object",
                            "description": "One bucket count row. Owner: secrets summary handler.", "properties": {"bucket": {"type": "string", "description": "Gap-type bucket emitted by secrets.IAMBucketCount.Bucket."}, "count": {"type": "integer", "description": "Rows in this bucket emitted by secrets.IAMBucketCount.Count."}}, "required": ["bucket", "count"]}},
                "s3_external_principal_grant_posture": {"type": "object", "description": "S3 external-principal grant posture counts read from the canonical GRANTS_ACCESS_TO graph edges, emitted by the secrets summary handler from IAMPostureSummary.S3ExternalPrincipalGrantPosture. Omitted on deployments without a graph reader.", "properties": {
                  "total_grants": {"type": "integer", "description": "Total grant edges emitted by secrets.IAMGrantPosture.TotalGrants (the grant_outcome sum, since every edge lands in exactly one outcome bucket)."},
                  "grants_by_outcome": {"type": "array", "description": "Grant counts grouped by grant outcome, emitted by secrets.IAMGrantPosture.GrantsByOutcome.", "items": {"type": "object",
                            "description": "One bucket count row. Owner: secrets summary handler.", "properties": {"bucket": {"type": "string", "description": "Grant-outcome bucket emitted by secrets.IAMBucketCount.Bucket."}, "count": {"type": "integer", "description": "Edges in this bucket emitted by secrets.IAMBucketCount.Count."}}, "required": ["bucket", "count"]}},
                  "grants_by_resolution_mode": {"type": "array", "description": "Grant counts grouped by resolution mode, emitted by secrets.IAMGrantPosture.GrantsByResolutionMode.", "items": {"type": "object",
                            "description": "One bucket count row. Owner: secrets summary handler.", "properties": {"bucket": {"type": "string", "description": "Resolution-mode bucket emitted by secrets.IAMBucketCount.Bucket."}, "count": {"type": "integer", "description": "Edges in this bucket emitted by secrets.IAMBucketCount.Count."}}, "required": ["bucket", "count"]}},
                  "public_grants": {"type": "integer", "description": "Public grant tally emitted by secrets.IAMGrantPosture.PublicGrants."},
                  "cross_account_grants": {"type": "integer", "description": "Cross-account grant tally emitted by secrets.IAMGrantPosture.CrossAccountGrants."},
                  "service_principal_grants": {"type": "integer", "description": "Service-principal grant tally emitted by secrets.IAMGrantPosture.ServicePrincipalGrants."}
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
                      "description": "Page of identity trust-chain rows, emitted by the secrets handler from the reducer read model as []secrets.IAMIdentityTrustChainResult.",
                      "items": {
                        "type": "object",
                        "description": "One identity trust-chain row. Owner: secrets handler IAMIdentityTrustChainResult.",
                        "properties": {
                          "chain_id": {"type": "string", "description": "Chain identity emitted by secrets.IAMIdentityTrustChainResult.ChainID."},
                          "state": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"], "description": "Chain state emitted by secrets.IAMIdentityTrustChainResult.State (exact only when every hop resolves with explicit evidence)."},
                          "confidence": {"type": "string", "description": "Chain confidence emitted by secrets.IAMIdentityTrustChainResult.Confidence."},
                          "service_account_join_key": {"type": "string", "description": "ServiceAccount join-key fingerprint emitted by secrets.IAMIdentityTrustChainResult.ServiceAccountJoinKey."},
                          "workload_object_id": {"type": "string", "description": "Durable workload object identity emitted by secrets.IAMIdentityTrustChainResult.WorkloadObjectID."},
                          "workload_kind": {"type": "string", "description": "Workload kind emitted by secrets.IAMIdentityTrustChainResult.WorkloadKind."},
                          "iam_role_fingerprint": {"type": "string", "description": "IAM role fingerprint emitted by secrets.IAMIdentityTrustChainResult.IAMRoleFingerprint."},
                          "vault_role_join_key": {"type": "string", "description": "Vault role join key emitted by secrets.IAMIdentityTrustChainResult.VaultRoleJoinKey."},
                          "vault_mount_join_key": {"type": "string", "description": "Vault mount join key emitted by secrets.IAMIdentityTrustChainResult.VaultMountJoinKey."},
                          "vault_policy_join_keys": {"type": "array", "items": {"type": "string"}, "description": "Vault policy join keys emitted by secrets.IAMIdentityTrustChainResult.VaultPolicyJoinKeys."},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Backing evidence fact ids emitted by secrets.IAMIdentityTrustChainResult.EvidenceFactIDs."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence the read could not produce, emitted by secrets.IAMIdentityTrustChainResult.MissingEvidence."},
                          "source_scopes": {"type": "array", "items": {"type": "string"}, "description": "Source scopes contributing to the chain, emitted by secrets.IAMIdentityTrustChainResult.SourceScopes."},
                          "source_generations": {"type": "array", "items": {"type": "string"}, "description": "Source generations contributing to the chain, emitted by secrets.IAMIdentityTrustChainResult.SourceGenerations."}
                        },
                        "required": ["chain_id", "state"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the secrets handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the secrets handler from the required limit param."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the secrets handler from the lookahead row."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Continuation cursor for the next page (present only when truncated), emitted by the secrets handler from the last row.",
                      "properties": {
                        "after_chain_id": {"type": "string", "description": "Chain id of the last row, emitted by the secrets handler from the last IAMIdentityTrustChainResult.ChainID."}
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
"privilege_posture_observations": {"type": "array", "description": "Page of privilege posture observation rows, emitted by the secrets handler from the reducer read model as []secrets.IAMPrivilegePostureObservationResult.", "items": {"type": "object", "description": "One privilege posture observation. Owner: secrets handler IAMPrivilegePostureObservationResult.", "properties": {
                "observation_id": {"type": "string", "description": "Observation identity emitted by secrets.IAMPrivilegePostureObservationResult.ObservationID."},
                "risk_type": {"type": "string", "description": "Risk type emitted by secrets.IAMPrivilegePostureObservationResult.RiskType."},
                "severity": {"type": "string", "description": "Severity emitted by secrets.IAMPrivilegePostureObservationResult.Severity."},
                "state": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"], "description": "Observation state emitted by secrets.IAMPrivilegePostureObservationResult.State."},
                "confidence": {"type": "string", "description": "Observation confidence emitted by secrets.IAMPrivilegePostureObservationResult.Confidence."},
                "subject_fingerprint": {"type": "string", "description": "Subject fingerprint (never a raw identity) emitted by secrets.IAMPrivilegePostureObservationResult.SubjectFingerprint."},
                "reason": {"type": "string", "description": "Machine-readable reason emitted by secrets.IAMPrivilegePostureObservationResult.Reason."},
                "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Backing evidence fact ids emitted by secrets.IAMPrivilegePostureObservationResult.EvidenceFactIDs."}
              }, "required": ["observation_id", "state"]}},
              "count": {"type": "integer", "description": "Rows returned in this page, emitted by the secrets handler as len(results)."}, "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the secrets handler from the required limit param."}, "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the secrets handler from the lookahead row."},
              "next_cursor": {"type": "object", "description": "Continuation cursor for the next page (present only when truncated), emitted by the secrets handler from the last row.", "properties": {"after_observation_id": {"type": "string", "description": "Observation id of the last row, emitted by the secrets handler from the last IAMPrivilegePostureObservationResult.ObservationID."}}, "required": ["after_observation_id"]}
            }, "required": ["privilege_posture_observations", "count", "limit", "truncated"]}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
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
"secret_access_paths": {"type": "array", "description": "Page of Vault policy-to-KV access path rows, emitted by the secrets handler from the reducer read model as []secrets.IAMSecretAccessPathResult.", "items": {"type": "object", "description": "One Vault policy-to-KV access path row. Owner: secrets handler IAMSecretAccessPathResult.", "properties": {
                "path_id": {"type": "string", "description": "Path identity emitted by secrets.IAMSecretAccessPathResult.PathID."},
                "chain_id": {"type": "string", "description": "Parent identity trust-chain emitted by secrets.IAMSecretAccessPathResult.ChainID."},
                "state": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"], "description": "Path state emitted by secrets.IAMSecretAccessPathResult.State."},
                "confidence": {"type": "string", "description": "Path confidence emitted by secrets.IAMSecretAccessPathResult.Confidence."},
                "kv_path_fingerprint": {"type": "string", "description": "KV path fingerprint (never a value) emitted by secrets.IAMSecretAccessPathResult.KVPathFingerprint."},
                "vault_mount_join_key": {"type": "string", "description": "Vault mount join key emitted by secrets.IAMSecretAccessPathResult.VaultMountJoinKey."},
                "vault_policy_join_key": {"type": "string", "description": "Vault policy join key emitted by secrets.IAMSecretAccessPathResult.VaultPolicyJoinKey."},
                "capabilities": {"type": "array", "items": {"type": "string"}, "description": "Vault capabilities emitted by secrets.IAMSecretAccessPathResult.Capabilities."},
                "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Backing evidence fact ids emitted by secrets.IAMSecretAccessPathResult.EvidenceFactIDs."}
              }, "required": ["path_id", "state"]}},
              "count": {"type": "integer", "description": "Rows returned in this page, emitted by the secrets handler as len(results)."}, "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the secrets handler from the required limit param."}, "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the secrets handler from the lookahead row."},
              "next_cursor": {"type": "object", "description": "Continuation cursor for the next page (present only when truncated), emitted by the secrets handler from the last row.", "properties": {"after_path_id": {"type": "string", "description": "Path id of the last row, emitted by the secrets handler from the last IAMSecretAccessPathResult.PathID."}}, "required": ["after_path_id"]}
            }, "required": ["secret_access_paths", "count", "limit", "truncated"]}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
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
"posture_gaps": {"type": "array", "description": "Page of posture gap rows, emitted by the secrets handler from the reducer read model as []secrets.IAMPostureGapResult.", "items": {"type": "object", "description": "One posture gap row. Owner: secrets handler IAMPostureGapResult.", "properties": {
                "gap_id": {"type": "string", "description": "Gap identity emitted by secrets.IAMPostureGapResult.GapID."},
                "gap_type": {"type": "string", "description": "Gap type emitted by secrets.IAMPostureGapResult.GapType."},
                "state": {"type": "string", "enum": ["exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"], "description": "Gap state emitted by secrets.IAMPostureGapResult.State."},
                "reason": {"type": "string", "description": "Machine-readable reason emitted by secrets.IAMPostureGapResult.Reason."},
                "service_account_join_key": {"type": "string", "description": "ServiceAccount join-key fingerprint emitted by secrets.IAMPostureGapResult.ServiceAccountJoinKey."},
                "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Backing evidence fact ids emitted by secrets.IAMPostureGapResult.EvidenceFactIDs."},
                "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence blocking exact truth, emitted by secrets.IAMPostureGapResult.MissingEvidence."},
                "unsupported_layers": {"type": "array", "items": {"type": "string"}, "description": "Unsupported layers emitted by secrets.IAMPostureGapResult.UnsupportedLayers."}
              }, "required": ["gap_id", "state"]}},
              "count": {"type": "integer", "description": "Rows returned in this page, emitted by the secrets handler as len(results)."}, "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the secrets handler from the required limit param."}, "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the secrets handler from the lookahead row."},
              "next_cursor": {"type": "object", "description": "Continuation cursor for the next page (present only when truncated), emitted by the secrets handler from the last row.", "properties": {"after_gap_id": {"type": "string", "description": "Gap id of the last row, emitted by the secrets handler from the last IAMPostureGapResult.GapID."}}, "required": ["after_gap_id"]}
            }, "required": ["posture_gaps", "count", "limit", "truncated"]}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
