# Loki Collector — Extraction Prep Census (`#8860`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the `#4047` cutover gate
can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-loki/` and
`go/internal/collector/observability/loki/`. Out of scope:
`sdk/go` (Codex `#8851`, already landed: `go.mod` pins
`eshu-sdk/collector v0.1.0-rc.3` + `eshu-sdk/factschema v0.1.0-rc.5`),
all other collectors, org repos, deployment.

## 1. Current packages

### Binary: `go/cmd/collector-loki/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:25,40` | entry point, `-mode=claimed-live\|cassette` | `parseArgs`, `run` |
| `config.go:58` | env config, `ESHU_COLLECTOR_INSTANCES_JSON` target shape | `loadClaimedRuntimeConfig`, `mapTarget` |
| `service.go:55` | claimed wiring | `buildClaimedService` |
| `service.go:31` | credential-free replay wiring | `buildCassetteService` |
| `service.go:93` | claim ID mint | `newClaimID` |
| `doc.go` | package contract | — |
| `config_test.go` | env/instance selection tests | — |

No unclaimed poll mode exists: the binary only runs claimed-live or
cassette replay. The auth token resolves exclusively through `token_env`
(`optionalEnvValue`, `config.go:201-211`); no inline token field exists.
Tenant identity may appear inline via `tenant_id` or resolve via
`tenant_id_env` (`optionalEnvOverride`, `config.go:213-223`), so raw
tenant IDs may appear in configuration. Disabled targets are skipped
before client construction (`source.go:52`).

### Library: `go/internal/collector/observability/loki/`

| File | Role | Key symbol |
| --- | --- | --- |
| `types.go:19` | collector family name | `CollectorKind = "loki"` |
| `types.go:21-23` | providers, scope kind | `ProviderLoki`, `ScopeKindLogSource` |
| `types.go:104-121` | warning vocabulary | `WarningPermissionHidden` … `WarningTruncated` |
| `types.go:124-181` | contract structs | `EnvelopeContext`, `SourceInstance`, `LogSignal`, `Rule`, `Warning` |
| `envelope.go:18,60,116,183` | 4 envelope constructors | `NewSourceInstanceEnvelope` … `NewCoverageWarningEnvelope` |
| `source.go:85` | claim-scoped source | `ClaimedSource.NextClaimed` |
| `source.go:160-175` | failure classification | `classifiedProviderFailure` |
| `http_client.go:28-30` | endpoint vocabulary | `/loki/api/v1/labels`, `/loki/api/v1/series`, `/loki/api/v1/rules` |
| `http_client.go:337` | endpoint failure tokens | `lokiOperationForEndpoint` |
| `http_client.go:391-395` | auth headers | `Authorization: Bearer`, `X-Scope-OrgID` |
| `redaction.go:13` | metadata-only enforcement | `fingerprint` (sha256, joined-key variant) |
| `metrics.go` | bounded emission counters | `recordFetch` … `recordHighCardinality` |
| `doc.go` | package contract | — |

## 2. Fact kinds and versions

Declared in `go/internal/facts/observability.go`
(`ObservabilityFamilyDeclaration`, `1.0.0` for all observability kinds).
This collector emits 4 of them, each at schema `1.0.0`:

| Kind | Version | Constructor |
| --- | --- | --- |
| `observability.source_instance` | `1.0.0` | `envelope.go:18` |
| `observability.observed_log_signal` | `1.0.0` | `envelope.go:60` |
| `observability.observed_rule` | `1.0.0` | `envelope.go:116` |
| `observability.coverage_warning` | `1.0.0` | `envelope.go:183` |

Producer record is already cutover-shaped
(`observability.go:165`, kinds declared `observability.go:88-110`):
`collector-loki`, authority `core_issued_grant`, planned repo
`eshu-hq/eshu-collector-loki`, in-tree path
`go/internal/collector/observability/loki`. The record names the primary
`observed_log_signal` kind; the grant draft covers all 4 emitted kinds
(see [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md)).

Shared-kind note: `observability.source_instance` and
`observability.coverage_warning` are also emitted by the grafana,
prometheus-mimir, and tempo collectors for their own providers, and
`observability.observed_rule` is shared with grafana and prometheus-mimir.
The producer grant binds identity + kind + schema + scope, so the
extracted `loki` producer cannot emit under a sibling's authority.

## 3. Consumers (must agree at cutover)

- Canonical projection:
  `go/internal/projector/observability/coverage/correlation_intents.go`.
- Factschema decode:
  `go/internal/projector/observability/coverage/factschema_decode_aws.go`
  and `materialization/`.
- Reducer domain: `observability_coverage_correlation`
  (truth profile `provider_gated`, `observability.go:137-141`).
- Query/read: `GET /api/v0/observability/coverage/correlations`
  (per `ObservabilityFamilyDeclaration.ReadSurface`).
- Coordinator: `go/internal/coordinator/loki_service.go` plans claim rows
  per active instance; stays core-owned.
- Golden cassette `testdata/cassettes/loki/supply-chain-demo.json`
  (`"collector": "loki"`, 1 scope) is replayed by the golden-corpus gate;
  the parity pack in `testdata/8860-parity-loki.json` (2 scopes) is the
  cutover comparison input.

## 4. Durable-state owner

The collector owns **no** durable state. Durable writes stay core-owned:

- Fact commit: `postgres.NewIngestionStore` (`service.go:42,72`).
- Claim control: `postgres.NewWorkflowControlStore` (`service.go:76`).
- Claim lifecycle: `collector.ClaimedService`
  (`go/internal/collector/claimed_service.go`).
- In-tree transient state only: `targets` map built at construction and
  per-call `CollectionStats`; both reset per process and never persist.

Admission, persistence, reducer queue, projection, and retraction remain Eshu
core per the extraction policy. The external producer must submit a bounded
result; it never receives Postgres, graph, queue, lease, reducer, API, MCP,
or workflow handles.

## 5. Protocols

- Loki REST: `GET /loki/api/v1/labels`, `GET /loki/api/v1/label/<name>/values`,
  `GET /loki/api/v1/series` (bounded by `defaultSeriesLookback = 24h` unless
  `series_lookback` is configured; never inherits `stale_after`),
  `GET /loki/api/v1/rules` (YAML rule groups).
- Auth: optional `Authorization: Bearer` from `token_env`;
  `X-Scope-OrgID` tenant header from `tenant_id`/`tenant_id_env`. Facts keep
  tenant presence plus a SHA-256 fingerprint, never the raw tenant value
  (`http_client.go:133-136`, `envelope.go:30-36`).
- Target wire shape: `ESHU_COLLECTOR_INSTANCES_JSON` instance
  `Configuration.targets[]` (`config.go:35-55`); runtime shape
  `TargetConfig` (`types.go:229-252`).
- Public target contract: `collector-sdk/v1alpha1`
  (`sdk/go/collector/types.go:10`, consumed via the published
  `eshu-sdk/collector v0.1.0-rc.3` module).
- Cassette replay: `File` load and validate (`ParseAndValidate` in
  `go/internal/replay/cassette/format.go`); binary flag
  `-mode=cassette -cassette-file=<path>` (`main.go:44-58`).
- Bounded retries: `maxHTTPRetries = 2` (`http_client.go:27`); 429s count
  `Stats.RateLimits`, all retries count `Stats.Retries`.

## 6. Artifacts

- Today: single in-tree binary `collector-loki`
  (`runtimeName` in `main.go:25`). No published artifact.
- At cutover: digest-pinned OCI artifact executed via
  `OCIRunner` in `go/internal/collector/extensionhost/oci.go`, same SDK
  contract as `ProcessRunner`. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` (diagnostic reads `extraction_candidate`). Not `ready_for_cutover`:
repository, release, operational, and rollback proof are absent. The SDK
surface is landed (`#8851` closed; qualified pins
collector `v0.1.0-rc.3` + factschema `v0.1.0-rc.5` in `go.mod`).
Roadmap owners: `#6707` inventory, `#6708` SDK/fact-schema,
`#6709` producer delegation, `#6710` template and compatibility harness,
`#4047` per-collector cutover gate.

## 8. Prerequisites for cutover

1. Published SDK + fact-schema modules (`#6708`, `#8851` Codex lane) —
   DONE, pins above.
2. First-party producer delegation live (`#6709`); grant draft recorded in
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
3. Collector repository from the `#6710` template with the compatibility
   harness matrix (newest collector × oldest core and reverse).
4. Parity proof on representative inputs via the fixture pack in
   `testdata/8860-parity-loki.json` and
   `source_parity_8860_test.go` (AC3, in-tree side).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)).

No-Observability-Change: this PREP adds no signals; the existing
`eshu_dp_loki_*` counters, fetch-duration histogram, and the two
`loki.observe` / `loki.fetch` spans
(`telemetry/contract/observability/loki.go`) already cover the claimed path.
The external producer must preserve them verbatim.
