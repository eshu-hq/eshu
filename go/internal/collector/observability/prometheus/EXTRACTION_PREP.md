# Prometheus/Mimir Collector — Extraction Prep Census (`#8864`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the cutover gate
(`#4047`) can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-prometheus-mimir/` and
`go/internal/collector/observability/prometheus/`. Out of scope:
`sdk/go` (Codex `#8851`), all other collectors, org repos, deployment.
`go/cmd/mock-prometheus-mimir/` stays in-tree: it serves `query_range` for
the golden-corpus API read path, not this collector's metadata ingest path.

## 1. Current packages

### Binary: `go/cmd/collector-prometheus-mimir/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:25,71` | entry point, `-mode=claimed-live\|cassette` | `parseArgs`, `run` |
| `config.go:18` | env config, `ESHU_COLLECTOR_INSTANCES_JSON` target shape | `loadClaimedRuntimeConfig`, `mapTarget` |
| `service.go:55` | claimed wiring | `buildClaimedService` |
| `service.go:29` | credential-free replay wiring | `buildCassetteService` |
| `service.go:93` | claim ID mint | `newClaimID` |
| `doc.go` | package contract | — |
| `config_test.go` | env/instance selection tests | — |

No unclaimed poll mode exists: the binary only runs claimed-live or
cassette replay. Target secrets resolve exclusively through `token_env`
and `tenant_id_env` (`config.go:162-174`); raw tokens and tenant IDs never
appear in configuration.

### Library: `go/internal/collector/observability/prometheus/`

| File | Role | Key symbol |
| --- | --- | --- |
| `types.go:19` | collector family name | `CollectorKind = "prometheus_mimir"` |
| `types.go:20-26` | providers, scope kind | `ProviderPrometheus`, `ProviderMimir`, `ScopeKindMetricSource` |
| `types.go:104-120` | warning vocabulary | `WarningRateLimited` … `WarningTruncated` |
| `types.go:122-251` | contract structs | `EnvelopeContext`, `TargetConfig`, `SourceConfig`, `ClaimedSource` |
| `envelope.go:18,59,121,188` | 4 envelope constructors | `NewSourceInstanceEnvelope` … `NewCoverageWarningEnvelope` |
| `source.go:86` | claim-scoped source | `ClaimedSource.NextClaimed` |
| `source.go:160-175` | failure classification | `classifiedProviderFailure` |
| `http_client.go:23-24` | endpoint vocabulary | `/api/v1/targets`, `/api/v1/rules` |
| `http_client.go:305` | endpoint failure tokens | `prometheusOperationForEndpoint` |
| `http_client.go:316` | in-band failure map | `(*HTTPClient).handleStatus` |
| `redaction.go:13,53` | metadata-only enforcement | `fingerprint`, `setURLRedaction` |
| `metrics.go` | bounded emission counters | `recordFetch`, `recordFacts`, `recordRedactions` |
| `doc.go` | package contract | — |

Every production file is listed; there is no error-mapper split out like
the OCI `http_errors.go` follow-up (`#8870`) caught — failure mapping
lives in `source.go:160` and `http_client.go:316`.

## 2. Fact kinds and versions

Declared in `go/internal/facts/observability.go`
(`ObservabilityFamilyDeclaration`, `1.0.0` for all 18 observability kinds).
This collector emits 4 of them, each at schema `1.0.0`:

| Kind | Version | Constructor |
| --- | --- | --- |
| `observability.source_instance` | `1.0.0` | `envelope.go:18` |
| `observability.observed_target` | `1.0.0` | `envelope.go:59` |
| `observability.observed_rule` | `1.0.0` | `envelope.go:121` |
| `observability.coverage_warning` | `1.0.0` | `envelope.go:188` |

Producer record is already cutover-shaped
(`observability.go:166`, kinds declared `observability.go:143-161`):
`collector-prometheus-mimir`, authority `core_issued_grant`, exactly these
4 kinds, planned repo `eshu-hq/eshu-collector-prometheus-mimir`, in-tree
path `go/internal/collector/observability/prometheus`.

Shared-kind note: `observability.source_instance`,
`observability.coverage_warning`, and `observability.observed_rule` are
also emitted by the grafana, loki, and tempo collectors for their own
providers. The producer grant binds identity + kind + schema + scope, so
the extracted `prometheus_mimir` producer cannot emit under a sibling's
authority; see [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).

## 3. Consumers (must agree at cutover)

- Canonical projection:
  `go/internal/projector/observability/coverage/correlation_intents.go`.
- Factschema decode:
  `go/internal/projector/observability/coverage/factschema_decode_aws.go`
  and `materialization/`.
- Reducer domain: `observability_coverage_correlation`
  (`observability.go:137-138`, truth profile `provider_gated` at
  `observability.go:141`).
- Query/read: `GET /api/v0/observability/coverage/correlations`
  (`observability.go:139`).
- Coordinator: `go/internal/coordinator/prometheus_mimir_service.go` plans
  claim rows per active instance; stays core-owned.
- No golden cassette under `testdata/cassettes` names this collector; the
  parity pack in `testdata/8864-parity-prometheus-mimir.json` is the
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

- Prometheus-compatible REST: `GET /api/v1/targets?state=active` (Prometheus
  only — Mimir exposes no scrape-target state and is never asked),
  `GET /api/v1/rules` (both providers).
- Auth: optional `Authorization: Bearer` from `token_env`;
  `X-Scope-OrgID` tenant header from `tenant_id`/`tenant_id_env`. Facts keep
  tenant presence plus a SHA-256 fingerprint, never the raw tenant value.
- Target wire shape: `ESHU_COLLECTOR_INSTANCES_JSON` instance
  `Configuration.targets[]` (`config.go:35-53`); runtime shape
  `TargetConfig` (`types.go:229-242`).
- Public target contract: `collector-sdk/v1alpha1`
  (`sdk/go/collector/types.go:10`).
- Cassette replay: `File` load and validate (`ParseAndValidate` in
  `go/internal/replay/cassette/format.go`); binary flag
  `-mode=cassette -cassette-file=<path>` (`main.go`).
- Bounded retries: `maxHTTPRetries = 2` (`http_client.go:21`); 429s count
  `Stats.RateLimits`, all retries count `Stats.Retries`.

## 6. Artifacts

- Today: single in-tree binary `collector-prometheus-mimir`
  (`runtimeName` in `main.go:25`). No published artifact.
- At cutover: digest-pinned OCI artifact executed via
  `OCIRunner` in `go/internal/collector/extensionhost/oci.go`, same SDK
  contract as `ProcessRunner`. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` (diagnostic reads `extraction_candidate`,
`catalog.go:95`). Not `ready_for_cutover`: repository, release,
operational, and rollback proof are absent, and the public SDK surface is
owned by `#8851`. Roadmap owners: `#6707` inventory, `#6708` SDK/fact-schema,
`#6709` producer delegation, `#6710` template and compatibility harness,
`#4047` per-collector cutover gate.

## 8. Prerequisites for cutover

1. Published SDK + fact-schema modules (`#6708`, `#8851` Codex lane).
2. First-party producer delegation live (`#6709`); grant draft recorded in
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
3. Collector repository from the `#6710` template with the compatibility
   harness matrix (newest collector × oldest core and reverse).
4. Parity proof on representative inputs via the fixture pack in
   `testdata/8864-parity-prometheus-mimir.json`,
   `source_parity_8864_test.go`, and `source_parity_signals_8864_test.go`
   (AC3, in-tree side).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)).
