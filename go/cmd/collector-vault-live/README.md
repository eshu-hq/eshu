# collector-vault-live

Runs the read-only, metadata-only Vault collector for the secrets/IAM posture
lane (#25, #1356). It selects one enabled, claim-capable `vault_live` collector
instance from `ESHU_COLLECTOR_INSTANCES_JSON`, claims Vault target work,
builds a metadata-only `vaultapi` client per target, and commits redacted
source facts via the shared collector commit boundary.

## Metadata-only

The collector only reads Vault metadata (auth mounts/roles, ACL policies,
identity entities/aliases, KV v2 metadata, secret-engine mounts). The `vaultapi`
client rejects any KV `/data/` path by construction, and all paths/names are
fingerprinted by the `secretsiam` envelope builders before emission.

## Configuration (environment)

| Variable | Purpose |
| --- | --- |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Desired collector instances with one enabled claim-capable `vault_live` instance. |
| `ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID` | Required when more than one enabled Vault live instance exists. |
| `ESHU_VAULT_LIVE_REDACTION_KEY` | Deployment-scoped key for deterministic HMAC markers over Vault names and paths. Required. |
| `ESHU_VAULT_LIVE_POLL_INTERVAL` | Delay between empty claim polls. Defaults to `5m`. |
| `ESHU_VAULT_LIVE_CLAIM_LEASE_TTL` | Lease TTL for workflow claims. |
| `ESHU_VAULT_LIVE_HEARTBEAT_INTERVAL` | Heartbeat interval; must be less than the lease TTL. |
| `ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID` | Optional claim owner label. |

Target shape inside the selected instance configuration:

```json
{
  "targets": [{
    "vault_cluster_id": "vault-a",
    "namespace": "admin",
    "display_name": "Vault prod",
    "environment": "prod",
    "address": "https://vault.example.test:8200",
    "token_env": "VAULT_READONLY_TOKEN",
    "source_uri": "https://vault.example.test:8200",
    "fencing_token": 1
  }]
}
```

The read-only Vault token is not serialized in collector JSON. `token_env`
names the private environment variable holding the token. Standard Postgres env
(`runtime.OpenPostgres`) is also required for the workflow-control and commit
stores.

Provision the read-only Vault policy from
`docs/public/reference/vault-secrets-iam-permissions.md` and bind it to the
token referenced by `token_env`.

## Telemetry

The binary exposes `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
through the shared hosted runtime. Vault source telemetry includes
`eshu_dp_secrets_iam_source_api_calls_total{source="vault",operation,result}`,
`eshu_dp_secrets_iam_source_redactions_total{source="vault",field_class}`,
`eshu_dp_secrets_iam_source_facts_emitted_total{source="vault",fact_kind}`,
`eshu_dp_secrets_iam_source_scope_freshness_seconds{source="vault",scope_kind}`,
and `eshu_dp_secrets_iam_partial_scope_total{source="vault",reason}`. Shared
collector metrics use `collector_kind="vault_live"` and snapshots run under the
`vault_live.snapshot` span.
