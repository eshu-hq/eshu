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
per-family resilient: a single family's list failure (for example a
permission-scoped read) emits a redacted `secrets_iam_coverage_warning` fact
(`facts.SecretsIAMCoverageWarningFactKind`; `source_state=partial`,
`resource_scope=<family>`) and collection continues, so one denied family never
loses the whole generation — the partial state is explicit in the facts, both as
the coverage-warning fact and as a `partial` generation freshness hint, never
silently complete. Context cancellation and a malformed observation remain
fatal.

`SnapshotSource` (`snapshot.go`) is the serial driver used by fixture and direct
source tests: it implements `collector.Source.Next`, yielding one snapshot
generation per configured Vault target (scope kind `vault_cluster`, collector
kind `vault_live`) with a deterministic per-target scope/generation id.
`ClaimedSource` (`claimed_source.go`) is the production path: it resolves a
workflow work item into one configured target, uses the workflow generation id
and fencing token, and commits through `collector.ClaimedService` in
`cmd/collector-vault-live`. The live `vaultapi` client is a `net/http`, no-SDK
adapter implementing the `Client` seam.

`vaultlive.Config` requires deployment-scoped redaction-key material. Vault
names, paths, accessors, aliases, policy hashes, and warning metadata are
persisted as deterministic HMAC markers through the `secretsiam` envelope
builders. The source never reads KV `/data`, tokens, AppRole secret ids, or
secret values.

Source telemetry: the lane emits `eshu_dp_secrets_iam_source_facts_emitted_total`
`{source="vault",fact_kind}` (per emitted fact family),
`eshu_dp_secrets_iam_source_api_calls_total{source="vault",operation,result}`
(per Vault list operation and outcome, via the `vaultapi` `OnAPICall` hook),
`eshu_dp_secrets_iam_partial_scope_total{source="vault",reason}` (per family with
partial coverage, where `reason` is the bounded family name),
`eshu_dp_secrets_iam_source_redactions_total{source="vault",field_class}` (one
increment per credential-bearing URI component stripped at the
`sanitizeVaultSourceURI` redaction site, where `field_class` is one of
`uri_userinfo`, `uri_query`, `uri_fragment`), and the
`eshu_dp_secrets_iam_source_scope_freshness_seconds{source="vault",scope_kind}`
gauge (generation age — `now` minus the generation `observed_at` — recorded at
finalization in `SnapshotSource`, complementing the `partial` freshness hint).
These complement the shared `collector_kind="vault_live"`
facts-emitted/commit/duration metrics and the `vault_live.snapshot` span.

The `field_class` and freshness labels are bounded enums: the redaction counter
names the stripped field *shape*, never its value, and the freshness gauge is
labeled by the bounded `scope_kind` (`vault_cluster`), never a cluster id,
namespace, or path.

## Evidence

No-Regression Evidence: the package is a read-only mapping lane plus serial
snapshot and claim-driven drivers. The mapping (`Source.Collect`) is single-pass
and per-family resilient (a family list error becomes a coverage-warning fact,
not a generation failure) with `slices.Grow` pre-sizing. `ClaimedSource` uses one
workflow work item per target, the target scope id as the durable conflict
domain, and the shared `collector.ClaimedService` claim, heartbeat, retry, and
commit boundary. It issues no Cypher, performs no graph or canonical writes, and
holds no graph locks; the only fan-out is the bounded per-target Vault metadata
read in the merged `vaultapi` client (depth/total-paths/body-size capped).
Correctness is covered by `go test ./internal/collector/vaultlive` (all-seven
family emission, the full redaction canary, SourceURI sanitization, the
metadata-only Client surface guard, claimed generation/fencing, per-target
generation scope/identity, batch drain/reset, redaction-key validation, and
deterministic namespace-scoped scope ids) and `go test ./cmd/collector-vault-live`
(claim config, token-from-env parsing, redaction-key loading, and claim lease
validation).

Observability Evidence: the lane emits the secrets/IAM source counters
`eshu_dp_secrets_iam_source_facts_emitted_total{source="vault",fact_kind}` (in
`SnapshotSource`) and `eshu_dp_secrets_iam_source_api_calls_total`
`{source="vault",operation,result}` (via the `vaultapi` `OnAPICall` hook,
asserted by `TestAdapterReportsAPICallObservations`). All metric labels are
bounded enums — no path, token, ARN, or address. These complement the shared
`collector.Service` metrics labeled `collector_kind="vault_live"`
(`eshu_dp_facts_emitted_total`, `eshu_dp_facts_committed_total`, the collector
observe duration) and the `vault_live.snapshot` span. It also emits
`eshu_dp_secrets_iam_partial_scope_total{source="vault",reason}` from the
per-family coverage warnings (`reason` = bounded family name), asserted by
`TestCollectIsResilientToOneFamilyFailure`. It emits
`eshu_dp_secrets_iam_source_redactions_total{source="vault",field_class}` at the
`sanitizeVaultSourceURI` redaction site (asserted by
`TestCollectRecordsURIRedactionsByFieldClass`, with
`TestCollectRecordsNoRedactionForCleanURI` proving a clean URI redacts nothing)
and the `eshu_dp_secrets_iam_source_scope_freshness_seconds{source="vault",scope_kind}`
gauge at generation finalization (asserted by `TestSnapshotRecordsScopeFreshness`).
All labels are bounded enums — no path, token, ARN, address, cluster id, or
namespace. The freshness gauge lives in `SnapshotSource`, where generations are
finalized, because the lane's freshness signal is produced there; the status
surface consumes generation freshness through the shared status report rather
than this collector-owned gauge.
