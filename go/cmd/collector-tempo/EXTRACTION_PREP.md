# Extraction PREP Census Lock — collector-tempo (#8867)

Bounded preparation inventory for extracting `collector-tempo` to
`eshu-hq/eshu-collector-tempo`. This file locks the current in-tree
census; it authorizes no move, no deployment, and no in-tree deletion.
Cutover stays gated by #4047 with production acceptance NOT_CHECKED.

Scope of this PREP: `go/cmd/collector-tempo/`,
`go/internal/collector/observability/tempo/`. Out of scope: `sdk/go`
(relocated under #8851, already consumed via `eshu-sdk` imports),
all other collectors, org repos, deployment.

## 1. Command surface

- `go/cmd/collector-tempo/main.go` (`func main`) entry: `--version`
  flag handling (`eshu-collector-tempo` binary name), telemetry
  bootstrap, `parseArgs` (`-mode claimed-live|cassette`,
  `-cassette-file` required in cassette mode), then `run()` which wires
  telemetry providers, optional pprof, Postgres, and
  `app.NewHostedWithStatusServer` before serving.
- `go/cmd/collector-tempo/service.go` (`buildClaimedService`): loads
  claimed runtime config, wires `tempo.NewClaimedSource`, commits
  through `postgres.NewIngestionStore`, returns
  `collector.ClaimedService` with `CollectorKind:
  scope.CollectorKind(tempo.CollectorKind)` (`"tempo"`, matching
  `scope.CollectorTempo`), claim lease TTL, heartbeat interval,
  `MaxAttempts`, `workflow.DefaultClaimMaxAttempts()`. Claim-aware
  only: no direct fact writes, no graph writes. `buildCassetteService`
  wires the credential-free cassette replay path
  (`cassette.NewSource`, 24h poll); no live Tempo credentials needed.
- `go/cmd/collector-tempo/config.go`
  (`loadClaimedRuntimeConfig`): parses `ESHU_COLLECTOR_INSTANCES_JSON`,
  selects the single enabled claim-driven `tempo` instance (ambiguous
  multi-instance without `ESHU_TEMPO_COLLECTOR_INSTANCE_ID` fails
  closed), maps targets/derived targets, resolves poll / lease /
  heartbeat durations, rejects heartbeat >= lease.
- `config.go` credential handling differs from the grafana lane on
  purpose: the token is OPTIONAL. `mapTarget` resolves
  `target.TokenEnv` through `optionalEnvValue` (empty `token_env`
  means unauthenticated Tempo; a named-but-unset variable fails
  closed), and `TenantID`/`TenantIDEnv` through `optionalEnvOverride`
  (inline tenant or env reference; a named-but-unset variable fails
  closed). The token travels in the `Authorization: Bearer` header and
  the tenant in `X-Scope-OrgID`; neither reaches desired collector
  config output, facts, logs, metric labels, or docs (see
  `config_test.go:
  TestLoadClaimedRuntimeConfigSelectsTempoInstanceAndResolvesTarget`
  and `extraction_prep_mirror_test.go:
  TestExtractionPrepCredentialResolutionFailsClosed`).

## 2. Collector libraries

`go/internal/collector/observability/tempo/`:

| File(s) | Role |
|---|---|
| `client.go` | Bounded REST reads: `GET /api/echo` (freshness probe, only when `FreshnessProbeEnable`), `GET /api/v2/search/tags`, `GET /api/v2/search/tag/{name}/values`; bearer + tenant headers; bounded retries (`maxHTTPRetries = 2`); per-endpoint failure tokens `probe_echo` / `list_tags` / `list_tag_values` (#8258) |
| `http_normalize.go` | `normalizeTagSet` / `normalizeTagValues`: tag union + unscoped dedup + sort; defaults `resource_limit` 100 (cap 500), `max_tag_values_per_tag` 20 (cap 100), `lookback` 1h; `declaredMatchState` from `DeclaredIDs` |
| `source.go` | `ClaimedSource` / `NewClaimedSource`: claim-to-fact fan-out, disabled targets skipped before client construction; `NextClaimed` is the ONLY emission path |
| `envelope.go` | Three envelope constructors: stable keys, generation binding, contract-payload merge via `eshu-sdk/factschema` (`EncodeObservabilitySourceInstance`, `EncodeObservabilityObservedTraceSignal`, `EncodeObservabilityCoverageWarning`) |
| `types.go` | `CollectorKind` (`"tempo"`), `ProviderTempo`, `ScopeKindTraceSource`, signal/freshness/outcome/warning vocabularies, `TargetConfig`, `SourceConfig`, `EvidenceClient` |
| `redaction.go` | Metadata-only redaction: sha256 fingerprints for tenants/tag values, `*_redacted` flags, `RedactionVersion = "observability-live-tempo-v1"`; raw TraceQL, tag values, and tenant IDs never persist |
| `metrics.go` | Bounded emission: provider requests, fetch duration, facts by kind, rate limits, retries, redactions, stale, high-cardinality rejections |
| `doc.go` | Godoc package contract |

Failure classification (`handleStatus` + `classifiedProviderFailure`):
403/401 append a `permission_hidden` warning and degrade to partial
(per endpoint, independently); 404/405 append `unsupported`; 429
appends `rate_limited` and returns a retryable provider error;
anything else returns a terminal provider error. Unknown claim
scope IDs fail as retryable provider failures; instance/kind/
generation mismatches fail closed before any fetch.

## 3. Durable source state (DDL + owner)

- No dedicated Tempo source-state table exists: there is no
  `*tempo*` migration under
  `go/internal/storage/postgres/migrations/`. Durable claim/lease
  state lives in the shared workflow control store
  (`postgres.NewWorkflowControlStore`); admission and status reads stay
  with core (`postgres.NewIngestionStore`, instrumented status store).
- Ownership split: the `tempo` package owns target validation,
  normalization, envelope construction, and source-local failure
  classification only. Post-extraction the extracted collector keeps
  that source ownership via the core-issued grant path; core keeps
  admission, projection, persistence, claim mutation, fencing, and the
  status read.

## 4. Fact kinds + versions

Three emitted kinds live in `go/internal/facts/observability.go`, each at
`ObservabilitySchemaVersionV1 = "1.0.0"`:

1. `observability.source_instance`
2. `observability.observed_trace_signal` (tag sets, tag values,
   freshness probes — metadata only, no spans)
3. `observability.coverage_warning` (rate-limited, permission-hidden,
   unsupported, partial, stale, high-cardinality)

Family: live Tempo metadata, `source_class=observed`,
`source_kind=tempo`; lifecycle owner `go/internal/facts`; reducer
domain mapping `declared_trace_route`/`observed_trace_signal` →
`"tempo"` in `obscoverage`; admission hook
`facts.ValidateSchemaVersion`. Producer split: `collector-tempo`
(core-issued grant, all 3 observed kinds) vs declared/GitOps evidence
(core-owned, preferred when current; live facts are fallback/
validation). Planned repository:
`eshu-collector-tempo`, authority mode `core_issued_grant` (family
declaration, `go/internal/facts/observability.go`).

## 5. Consumers + protocols

- Reducer: `go/internal/reducer/obscoverage/` binds observed
  trace-signal envelopes into coverage truth;
  declared/applied/observed comparison stays there.
- Coordinator: `go/internal/coordinator/planner/tempo/` plans `tempo`
  workflow rows from desired instances; `service_tempo_test.go` and
  `in_tree_schedulers.go` cover tempo scheduling; no collection logic
  there.
- Query surfaces read projected truth, never this package directly
  (`go/internal/query/contract/observability_coverage.go`, MCP
  dispatch coverage tests).
- Helm: `go/internal/runtime/helm_observability_collectors_contract_test.go`
  pins a `tempo-primary` instance shape for the in-tree chart path.
- Protocols: Tempo HTTP API (bearer header, `X-Scope-OrgID` tenant
  header; never query parameters); facts schema `1.0.0` via
  `eshu-sdk/factschema/observability/v1` (physical relocation under
  #8851 is COMPLETE and consumed here). Supported oldest/newest core
  and component version pairs: NOT_CHECKED until the first extracted
  release (`version-matrix.sh`).

## 6. Migration stage + prerequisites

- Stage: in-tree preparation (this PREP). Destination repository does
  not exist yet; no standalone release; no deployment.
- Prerequisites to reuse: #6708 SDK publication, #6709 producer
  delegation, #6710 standalone template, #4047 gate implementation,
  #8851 physical SDK relocation — ALL CLOSED.
- Blockers: destination repository creation, owner-designated QA EKS
  cutover rehearsal (#8968 chart carry), independent review + stable
  exact-head CI at promotion.
- Mirror proof: `extraction_prep_mirror_test.go` in both packages
  (admission at 1.0.0, determinism, generation rebind, redaction
  sentinel, fail-closed hostile inputs, claim boundary, credential
  resolution, heartbeat guard, claim-ID namespace) plus the
  byte-identical parity pack (`source_parity_8867_test.go` +
  `testdata/8867-parity-tempo.json`: live httptest run vs recorded
  golden, and the per-endpoint permission-hidden degradation shape).
