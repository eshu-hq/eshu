# #8869 collector-vault-live extraction census

## Scope

Base: `origin/main` at `51a15dc6b6`. Worktree
`/home/ubuntu/personal-repos/eshu-8869-vault-census`, branch
`drive/8869-vault-census`.

This record answers #8869 acceptance criterion 2 (packages, fact kinds,
consumers, durable-state ownership, protocols, artifacts, migration stage,
prerequisites) and the credential-handling half of criterion 7, for the
`collector-vault-live` to `eshu-collector-vault-live` cutover. It moves no
code and authorizes no release, deployment, rollback, or in-tree removal:
the destination repository `eshu-hq/eshu-collector-vault-live` does not
resolve in the organization inventory (verified 2026-10-27), so criteria 8-10
remain unmet by construction.

Migration stage: `planned`. Destination and owner are assigned by
`docs/internal/design/eshu-ecosystem-repository-migration.md` (row:
`collector-vault-live` to `eshu-collector-vault-live`); prerequisites remain.

## Source command

`go/cmd/collector-vault-live/` (`main.go`, `service.go`, `config.go`,
`doc.go`, `config_test.go`, plus `AGENTS.md`/`README.md`). Binary
`eshu-collector-vault-live`. Two launch modes: `claimed-live` (default) and
`cassette` (`-cassette-file`, credential-free, 24h poll).

`main.go` hosts via the shared app host with `/healthz`, `/readyz`,
`/metrics`, and `/admin/status`. `service.go:buildClaimedService` wires
`vaultlive.NewClaimedSource` onto the shared collector commit boundary
(`postgres.NewIngestionStore`) with `postgres.NewWorkflowControlStore` claims;
`buildCassetteService` wires `cassette.NewSource` with no Vault credentials.
The scope identity is `vault_live` on both sides of the boundary:
`scope.CollectorVaultLive` (see `go/internal/scope/collector_kind.go`, symbol
`CollectorVaultLive`) equals `vaultlive.CollectorKind` (see
`go/internal/collector/vaultlive/snapshot.go`, symbol `CollectorKind`);
`go/cmd/collector-vault-live/extraction_census_test.go` locks that agreement.

## Supporting packages

- `go/internal/collector/vaultlive/` (`claimed_source.go`, `source.go`,
  `snapshot.go`, `map.go`, `client.go`, `types.go`, plus `doc.go`): claim-aware
  source, snapshot scanner, fact builders, client seam, target types.
- `go/internal/collector/vaultlive/vaultapi/` (`client.go`, `kv.go`,
  `resources.go`, `vault_errors.go`, plus `doc.go`): read-only Vault HTTPS
  client. It is the metadata-only seam: KV `/data/` reads are rejected, so no
  code path can wire a secret-value read.
- `go/internal/collector/access/posture/`: secrets/IAM envelope builders the
  emitted facts flow through.
- Shared commit boundary (stays in core per the migration plan):
  `go/internal/collector` (`Service`, `ClaimedService`),
  `go/internal/replay/cassette`, `go/internal/scope`
  (`scope.CollectorVaultLive`), workflow claim/lease
  (`DefaultClaimMaxAttempts`, `DefaultClaimLeaseTTL`,
  `DefaultHeartbeatInterval`), `go/internal/storage/postgres`
  (`NewIngestionStore`, `NewWorkflowControlStore`,
  `NewInstrumentedStatusStore`), `go/internal/telemetry`,
  `go/internal/app`, `go/internal/runtime`.

## Fact kinds

Seven source facts plus one coverage warning, all metadata-only:

- `vault_auth_mount`
- `vault_auth_role`
- `vault_acl_policy`
- `vault_identity_entity`
- `vault_identity_alias`
- `vault_kv_metadata`
- `vault_secret_engine_mount`
- `SecretsIAMCoverageWarningFactKind` on partial scope
  (`snapshot.go:174-257`).

Builders live in `go/internal/collector/vaultlive/map.go`
(`mapAuthMount`, `mapAuthRole`, `mapACLPolicy`, `mapIdentityEntity`,
`mapIdentityAlias`, `mapKVMetadata`, `mapSecretEngineMount`). Names and paths
are HMAC-fingerprinted before emission; metric labels are bounded enums only
(`source="vault"`, operation, result).

## Consumers

- Reducer secrets-IAM graph projection (`go/internal/reducer/*secretsiam*`,
  including the additive-domains drift defaults).
- In-tree scheduler `go/internal/coordinator/vault/live/planner.go` (with
  `in_tree_schedulers.go`).
- Status and promotion catalog (`promotion_catalog.go`) and the
  capability/capability-inventory surfaces.

## Durable-state ownership

Core Postgres owns all durable state. The collector owns no tables:

- fact commits: `postgres.NewIngestionStore`;
- claims and leases: `postgres.NewWorkflowControlStore`;
- health and status: `postgres.NewInstrumentedStatusStore`.

Per the migration plan, facts, generations, admission, recovery, canonical
graph, workflow plans, claims, and scheduler state stay core-owned after
extraction; the extracted runtime must use the versioned fact-submission and
claim contracts, never shared-table shortcuts.

## Protocols

- Vault HTTPS API with a read-only token; list/get on metadata endpoints only.
- Postgres wire (core-owned stores above).
- OTLP traces and metrics (`SecretsIAMSourceFactsEmitted`, `PartialScope`,
  `ScopeFreshness`, `eshu_dp_vault_request_total`), Prometheus handler, pprof
  server, hosted status server.
- Configuration: `ESHU_COLLECTOR_INSTANCES_JSON` selects the one enabled
  claim-capable `vault_live` instance; per-target JSON carries
  `vault_cluster_id`, `namespace`, `address`, `token_env`, `source_uri`, and
  `fencing_token`. Runtime tuning via `ESHU_VAULT_LIVE_POLL_INTERVAL`,
  `ESHU_VAULT_LIVE_CLAIM_LEASE_TTL`, `ESHU_VAULT_LIVE_HEARTBEAT_INTERVAL`,
  `ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID`,
  `ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID`, and `ESHU_VAULT_LIVE_REDACTION_KEY`.

## Credential handling and redaction

- The token is never serialized in the targets JSON: `token_env` names the
  environment variable holding the read-only token
  (`config.go:30-42,162-186`). An empty resolved token fails closed.
- The cross-boundary `vaultlive.Config`/`vaultlive.ClusterTarget` carry
  non-secret identity only (`snapshot.go:34-52`); connection details and the
  token live in the command-local `vaultClientFactory`, keyed by
  `authKey(cluster, namespace)` because the scope identity is
  namespace-scoped (`service.go:109-153`).
- `ESHU_VAULT_LIVE_REDACTION_KEY` is required; names and paths are
  HMAC-fingerprinted with it. Heartbeat must stay below the claim-lease TTL.
- Do not log the token or the raw Vault address
  (`go/cmd/collector-vault-live/AGENTS.md`).
- `go/cmd/collector-vault-live/extraction_census_test.go` guards the
  boundary: no `token`/`secret`/`password`/`credential` field may appear on
  the cross-boundary structs (`FencingToken` is allowlisted only as a locked
  int64 generation counter), and `authKey` must separate both dimensions.

## Artifacts

- In-tree binary via the `main` package; no standalone release artifact.
- Helm: `deploy/helm/eshu/templates/deployment-vault-live-collector.yaml`
  (command `/usr/local/bin/eshu-collector-vault-live`, values-gated) and
  `service-vault-live-collector-metrics.yaml`.

## Prerequisites (closed, reused)

- #6708 SDK collector and fact-schema modules published.
- #6709 producer-grant authorization merged.
- #6710 standalone collector template and compatibility harness merged.
- #4047 extraction-readiness gate closed (8th criterion
  `payload_schemas_typed` enforced).

## TDD and focused proof

`go/cmd/collector-vault-live/extraction_census_test.go` was written first and
failed on the missing record (`no such file or directory`), then passed after
this record landed. Focused proof:

```text
cd go && go test -count=1 ./cmd/collector-vault-live/...
cd go && go test -count=1 ./internal/collector/vaultlive/...
```

## Unresolved (NOT_CHECKED, owned by later cutover steps)

- Named implementation, public-contract, release, and deployment owners; the
  production target and configuration source beyond the owner-confirmed QA
  designation in #8869.
- Oldest/newest supported core and component version pairs; vulnerability
  response record.
- Destination repository creation (out of grant; needs arbiter ruling before
  any independent build, release, or deploy).
- Parity, projection, concurrency, resource, credential, rollback-rehearsal,
  and production-acceptance proofs on the extracted path.

No-Regression Evidence: this record moves no code, alters no runtime behavior,
and claims no collector has cut over. The accompanying test adds assertions
only; it changes no production path.

No-Observability-Change: this record adds no metric, span, log, status field,
or dashboard label.
