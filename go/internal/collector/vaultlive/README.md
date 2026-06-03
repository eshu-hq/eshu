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

`SnapshotSource` (`snapshot.go`) is the runtime driver: it implements
`collector.Source.Next`, yielding one snapshot generation per configured Vault
target (scope kind `vault_cluster`, collector kind `vault_live`) with a
deterministic per-target scope/generation id. The live `vaultapi` client (a
`net/http`, no-SDK adapter) implements the `Client` seam, and the
`cmd/collector-vault-live` binary runs the driver over the shared
`collector.Service` commit boundary.

Remaining under #1356: the bespoke `eshu_dp_secrets_iam_*{source="vault"}` source
counters (the lane is already observable via the shared
`collector_kind="vault_live"` facts-emitted/commit/duration metrics and the
`vault_live.snapshot` span), per-family partial-coverage warnings, and
validation against a live/dev Vault.

## Evidence

No-Regression Evidence: the package is a read-only mapping lane plus a serial
snapshot driver. The mapping (`Source.Collect`) is single-pass and fail-fast per
family with `slices.Grow` pre-sizing; `SnapshotSource` is driven serially by
`collector.Service.Next` (one generation per target, the per-target scope id as
the durable conflict domain) and reuses the shared `collector.Service` commit
boundary unchanged. It issues no Cypher, performs no graph or canonical writes,
holds no locks/leases, and runs no queue or concurrent workers; the only fan-out
is the bounded per-target Vault metadata read in the merged `vaultapi` client
(depth/total-paths/body-size capped). So there is no new hot-path or backend
behavior to regress. Correctness is covered by
`go test ./internal/collector/vaultlive` (all-seven-family emission, the full
redaction canary, SourceURI sanitization, the metadata-only Client surface
guard, per-target generation scope/identity, batch drain/reset, config
validation, and deterministic namespace-scoped scope ids) and
`go test ./cmd/collector-vault-live` (config + token-from-env parsing). Live
throughput against a real Vault is validated as part of the remaining #1356
integration step.

Observability Evidence: the lane is observable through the shared
`collector.Service` metrics labeled `collector_kind="vault_live"`
(`eshu_dp_facts_emitted_total`, `eshu_dp_facts_committed_total`, the collector
observe duration) plus the `vault_live.snapshot` span (registered in the frozen
telemetry span contract). The bespoke `eshu_dp_secrets_iam_*{source="vault"}`
source counters (api calls, redactions, facts emitted, scope freshness, partial
scope) are a tracked #1356 follow-up.
