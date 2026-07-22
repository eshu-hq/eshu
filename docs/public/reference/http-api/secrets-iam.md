# HTTP Secrets/IAM Routes

Use these routes when a client needs reducer-owned secrets/IAM posture truth.
The routes read active reducer facts for one bounded scope or row anchor. They
never expose secret values, raw Vault paths, token claims, policy documents, or
graph-promoted paths.

!!! note "Secrets/IAM trust-chain graph projection is off by default"

    These read-model routes are always live: identity trust chains, privilege
    posture observations (including GCP service-account secret-access and
    broad-role grants), secret-access paths, and posture gaps are reducer-owned
    facts that do not depend on any graph projection. The separate **graph**
    projection of the `SecretsIAM*` node/edge families
    (`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`, default off) is gated
    behind the target-bound activation record and flag-on proof in #2430; it is
    **not** required for these routes. It is also independent of GCP cloud
    relationship edges and GCP cloud inventory, which are live regardless. See
    the internal activation runbook (ADR #1314) for the gate status and the
    steps required to enable the trust-chain graph projection.

## Route Map

| Route | Purpose |
| --- | --- |
| `GET /api/v0/secrets-iam/identity-trust-chains` | Lists identity trust chains by scope, chain, workload object, service account join key, IAM role fingerprint, or state. |
| `GET /api/v0/secrets-iam/privilege-posture-observations` | Lists reducer-owned risky or partial posture observations by scope, observation, risk type, severity, or state. |
| `GET /api/v0/secrets-iam/secret-access-paths` | Lists Vault policy-to-KV metadata access paths reachable from exact identity chains. |
| `GET /api/v0/secrets-iam/posture-gaps` | Lists missing, stale, permission-hidden, or unsupported evidence that blocks exact trust-chain truth. |
| `GET /api/v0/secrets-iam/posture-summary` | Returns grouped posture counts for one reducer scope. |

OpenAPI remains canonical for full request and response schemas.

## Shared Rules

List routes require `limit` and at least one bounded anchor. `limit` defaults
only in MCP tool dispatch; direct HTTP callers must send it. Valid state values
are `exact`, `partial`, `unresolved`, `stale`, `permission_hidden`, and
`unsupported`.

The posture summary route requires `scope_id` and does not accept pagination.
It returns provenance-only grouped counts:

- identity trust chains by state
- privilege posture observations by risk type
- privilege posture observations by severity
- secret access paths by state
- posture gaps by gap type
- S3 external-principal grant posture
  (`s3_external_principal_grant_posture`): total grants, grants by grant
  outcome, grants by resolution mode, and public / cross-account /
  service-principal tallies

Summary buckets use low-cardinality reducer payload fields. Empty or missing
bucket values are reported as `unknown` so callers can see data quality gaps
without inspecting raw evidence.

The grant posture section is read from the canonical
`(:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal)` graph edges
materialized by the `s3_external_principal_grant_materialization` reducer
domain, so it reflects materialized graph truth: unsupported grants and grants
whose source bucket never materialized are excluded. No principal identity,
ARN, or bucket name is returned — counts only. On deployments without a graph
reader the section is omitted from the summary.

## Identity Trust Chains

`GET /api/v0/secrets-iam/identity-trust-chains`

The caller must provide `limit` and at least one bounded anchor:

- `scope_id`
- `chain_id`
- `workload_object_id`
- `service_account_join_key`
- `iam_role_fingerprint`

Rows carry fingerprints, join keys, state, confidence, evidence fact IDs, and
missing-evidence markers. A chain is exact only when every hop is resolved with
explicit evidence. Partial, unresolved, stale, permission-hidden, or
unsupported chains remain provenance-only and are not promoted to graph truth.

## Privilege Posture Observations

`GET /api/v0/secrets-iam/privilege-posture-observations`

The caller must provide `limit` and either `scope_id` or `observation_id`.
Optional `risk_type`, `severity`, and `state` filters narrow an already bounded
request.

Rows describe reducer-owned broad or partial posture evidence, such as external
trust without an external ID. Subject identifiers are fingerprints. These rows
explain risk without becoming exact reachability or access-path truth.

## Secret Access Paths

`GET /api/v0/secrets-iam/secret-access-paths`

The caller must provide `limit` and at least one bounded anchor:

- `scope_id`
- `path_id`
- `chain_id`
- `vault_mount_join_key`

Rows describe Vault policy-to-KV metadata paths as fingerprints and
capabilities only. Secret values and raw paths are never returned.

## Posture Gaps

`GET /api/v0/secrets-iam/posture-gaps`

The caller must provide `limit` and at least one bounded anchor:

- `scope_id`
- `gap_id`
- `service_account_join_key`

Optional `gap_type` and `state` filters narrow an already bounded request. Rows
surface missing, stale, permission-hidden, or unsupported evidence rather than
dropping it from the trust-chain result.

## Posture Summary

`GET /api/v0/secrets-iam/posture-summary`

The caller must provide `scope_id`.

The response body includes `scope_id` plus `summary`, with these arrays of
`bucket` and `count` objects:

- `identity_trust_chains_by_state`
- `privilege_observations_by_risk_type`
- `privilege_observations_by_severity`
- `secret_access_paths_by_state`
- `posture_gaps_by_gap_type`

This route is a dashboard rollup over reducer-owned read models. It performs
bounded `GROUP BY` reads for one active reducer scope and exposes counts only.
