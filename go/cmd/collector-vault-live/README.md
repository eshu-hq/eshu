# collector-vault-live

Runs the read-only, metadata-only Vault collector for the secrets/IAM posture
lane (#25, #1356). It drives `vaultlive.SnapshotSource` over a configured set of
Vault targets, building a metadata-only `vaultapi` client per target, and
commits redacted source facts via the shared collector commit boundary.

## Metadata-only

The collector only reads Vault metadata (auth mounts/roles, ACL policies,
identity entities/aliases, KV v2 metadata, secret-engine mounts). The `vaultapi`
client rejects any KV `/data/` path by construction, and all paths/names are
fingerprinted by the `secretsiam` envelope builders before emission.

## Configuration (environment)

- `ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID` (required) — durable collector instance
  id stamped on every fact.
- `ESHU_VAULT_LIVE_TARGETS_JSON` (required) — `{"targets":[{...}]}` where each
  target is `{vault_cluster_id, namespace, display_name, environment, address,
  token_env, source_uri, fencing_token}`. **The token is not in this JSON**;
  `token_env` names the environment variable holding the read-only token.
- `ESHU_VAULT_LIVE_POLL_INTERVAL` (optional, default `5m`).
- Standard Postgres env (`runtime.OpenPostgres`) for the commit store.

Provision the read-only Vault policy from
`docs/public/reference/vault-secrets-iam-permissions.md` and bind it to the
token referenced by `token_env`.

## Status

The snapshot driver + client adapter are fixture-tested. Validation against a
live/dev Vault and the `eshu_dp_secrets_iam_*` source-specific counters are
tracked follow-ups; the collector is already observable via the shared
`collector_kind="vault_live"` facts-emitted/commit/duration metrics and the
`vault_live.snapshot` span.
