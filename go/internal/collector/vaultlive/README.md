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

## Evidence

No-Regression Evidence: this package is pure in-memory mapping from a read-only
`Client` to `secretsiam` fact envelopes. It issues no Cypher, performs no graph
or canonical writes, holds no locks/leases, and runs no concurrent workers or
queues — collection is single-pass and fail-fast per family, with `slices.Grow`
pre-sizing the one accumulation slice. There is no hot-path or backend behavior
to regress; correctness is covered by `go test ./internal/collector/vaultlive`
(all-seven-family emission, the full redaction canary, SourceURI sanitization,
and the metadata-only Client surface guard). The live `hashicorp/vault/api`
adapter, its API pagination/throttle profile, and benchmarks land in #1356,
which owns the performance contract for live Vault scans.

No-Observability-Change: this PR adds no telemetry instruments, spans, logs, or
status fields. The `eshu_dp_secrets_iam_*` source metrics (API calls,
redactions, facts emitted, scope freshness, partial scope) are introduced with
the live adapter in #1356; until then there is no runtime path to observe.
