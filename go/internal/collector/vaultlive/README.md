# vaultlive

`vaultlive` is the live HashiCorp Vault source lane for the secrets/IAM posture
collector family (issue #25). It observes Vault identity, trust, and
secret-metadata posture and emits redacted `secretsiam` source facts. It is the
read half of the family's Vault contract; the reducer owns all trust-chain
correlation and graph promotion.

## Responsibilities

- Read Vault **metadata only** through the `Client` seam: auth mounts and roles,
  ACL policies, identity entities/aliases, KV v2 metadata, and secret-engine
  mounts.
- Map each observation to a `secretsiam` envelope builder, which fingerprints
  mount paths, key names, and accessors before emission.
- Emit Eshu fact envelopes for one bounded `VaultTarget` scope
  (`{vault_cluster_id, namespace}`) under a coordinator-assigned generation.

## Hard boundaries (metadata-only)

- Never read a secret value: no Vault KV `/data` endpoint, no Secrets Manager /
  SSM decrypted value, no Kubernetes Secret `.data`.
- Never persist tokens, AppRole `secret_id`, OIDC client secrets, private keys,
  or bearer tokens — in facts, logs, or metrics.
- The `Client` interface deliberately exposes no value-reading method; a
  structural test (`TestClientSurfaceIsMetadataOnly`) guards this invariant.
- Mount paths and key names are hashed by default via the `secretsiam` envelope
  builders; cleartext is never emitted from this lane.

## Boundaries with the rest of Eshu

- Collectors observe source truth; the reducer decides graph truth. This package
  performs no graph writes and no correlation.
- Trust-chain correlation (IAM ↔ Kubernetes ↔ Vault) and posture findings are
  owned by the reducer domain `DomainSecretsIAMTrustChain`.

## Status

Maps all seven Vault metadata fact families (`vault_auth_mount`,
`vault_auth_role`, `vault_acl_policy`, `vault_identity_entity`,
`vault_identity_alias`, `vault_kv_metadata`, `vault_secret_engine_mount`) from a
read-only `Client` through the `secretsiam` envelope builders. Collection is
fail-fast per family so a partial generation is never emitted as if complete.

The live `Client` adapter (`hashicorp/vault/api`), `runtimebind` registration,
the `secrets_iam_posture` CollectorKind, claim-driven scheduling, per-family
partial-coverage warnings, and source telemetry instruments land in #1356.
