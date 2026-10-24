# Extraction PREP Census Lock — collector-grafana (#8857)

Bounded preparation inventory for extracting `collector-grafana` to
`eshu-hq/eshu-collector-grafana`. This file locks the current in-tree
census; it authorizes no move, no deployment, and no in-tree deletion.
Cutover stays gated by #4047 with production acceptance NOT_CHECKED.

Scope of this PREP: `go/cmd/collector-grafana/`,
`go/internal/collector/observability/grafana/`. Out of scope: `sdk/go`
(Codex #8851), all other collectors, org repos, deployment.

## 1. Command surface

- `go/cmd/collector-grafana/main.go` (`func main`) entry: `--version`
  flag handling, telemetry bootstrap, `parseArgs` (`-mode
  claimed-live|cassette`, `-cassette-file` required in cassette mode),
  then `run()`.
- `go/cmd/collector-grafana/service.go` (`buildClaimedService`): loads
  claimed runtime config, wires `grafana.NewClaimedSource`, commits
  through `postgres.NewIngestionStore`, returns
  `collector.ClaimedService` with `CollectorKind:
  scope.CollectorKind(grafana.CollectorKind)` (`"grafana"`), claim lease
  TTL, heartbeat interval, `MaxAttempts`,
  `DefaultClaimMaxAttempts()`. Claim-aware only: no direct fact writes,
  no graph writes. `buildCassetteService` wires the credential-free
  cassette replay path (`cassette.NewSource`); no live credentials needed.
- `go/cmd/collector-grafana/config.go`
  (`loadClaimedRuntimeConfig`): parses `ESHU_COLLECTOR_INSTANCES_JSON`,
  selects the single enabled claim-driven `grafana` instance (ambiguous
  multi-instance without `ESHU_GRAFANA_COLLECTOR_INSTANCE_ID` fails
  closed), maps targets/derived targets, resolves poll / lease /
  heartbeat durations, rejects heartbeat >= lease.
- `config.go` (`Token` via `requiredEnvValue` of `target.TokenEnv`): the
  Grafana token is resolved from the environment at startup into a
  runtime-only `TargetConfig` field. It is never copied into desired
  collector config output, facts, logs, metric labels, or docs (see
  `config_test.go:
  TestLoadClaimedRuntimeConfigSelectsGrafanaInstanceAndResolvesCredential`
  and `extraction_prep_mirror_test.go:
  TestExtractionPrepClaimBoundaryRequiresClaimCapableInstance`).

## 2. Collector libraries

`go/internal/collector/observability/grafana/`:

| File(s) | Role |
|---|---|
| `client.go` | Bounded REST reads: `/api/search` (folders/dashboards), `/api/datasources`, `/api/v1/provisioning/alert-rules`; pagination bounded, `resource_limit` default 100 / max 500 |
| `source.go` | `ClaimedSource` / `NewClaimedSource`: claim-to-fact fan-out, disabled targets skipped before client construction |
| `envelope.go` | Four envelope constructors: stable keys, generation binding, contract-payload merge via `sdk/go/factschema` |
| `types.go` | `CollectorKind`, `TargetConfig`, `SourceConfig`, `Resource`, `AlertRule`, `Warning`, `CollectionStats`, outcome/freshness/warning vocabularies |
| `redaction.go` | Metadata-only redaction: sha256 fingerprints for titles/names/URLs, `*_redacted` flags, `redaction_state` |
| `metrics.go` | Bounded emission: provider requests, fetch duration, facts by kind, rate limits, retries, redactions |
| `doc.go` | Godoc package contract (metadata-only, four fact kinds, no declared/applied/observed comparison) |

## 3. Durable source state (DDL + owner)

- No dedicated Grafana source-state table exists: there is no
  `*grafana*` migration under
  `go/internal/storage/postgres/migrations/`. Durable claim/lease state
  lives in the shared workflow control store
  (`postgres.NewWorkflowControlStore`); admission and status reads stay
  with core (`postgres.NewIngestionStore`, instrumented status store).
- Ownership split: the `grafana` package owns target validation,
  normalization, envelope construction, and source-local failure
  classification only. Post-extraction the extracted collector keeps
  that source ownership via the core-issued grant path; core keeps
  admission, projection, persistence, claim mutation, fencing, and the
  status read.

## 4. Fact kinds + versions

Four emitted kinds live in `go/internal/facts/observability.go`, each at
`ObservabilitySchemaVersionV1 = "1.0.0"`:

1. `observability.source_instance`
2. `observability.observed_dashboard` (folders, dashboards, datasources)
3. `observability.observed_rule` (alert rules)
4. `observability.coverage_warning` (rate-limited, permission-hidden,
   unsupported, partial, stale, truncated)

Family: live Grafana metadata, `source_class=observed`,
`source_kind=grafana`; lifecycle owner `go/internal/facts`; reducer
domain `obscoverage`; admission hook `facts.ValidateSchemaVersion`.
Producer split: `collector-grafana` (core-issued grant, all 4 observed
kinds) vs declared/GitOps evidence (core-owned, preferred when current;
live facts are fallback/validation). Planned repository:
`eshu-collector-grafana`, authority mode `core_issued_grant` (see
`producer-grant.manifest.DRAFT.yaml`).

## 5. Consumers + protocols

- Reducer: `go/internal/reducer/obscoverage/` binds observed envelopes
  into coverage truth; declared/applied/observed comparison stays there.
- Coordinator: `go/internal/coordinator/grafana_service.go` +
  `planner/grafana/` plan `grafana` workflow rows from desired
  instances; no collection logic there.
- Query surfaces read projected truth, never this package directly.
- Protocols: Grafana HTTP API (token header, never query parameter);
  facts schema `1.0.0` via `sdk/go/factschema/observability/v1`
  (physical relocation under Codex #8851 is a prerequisite, not this
  PREP). Supported oldest/newest core pairs: NOT_CHECKED until the
  first extracted release (`version-matrix.sh`).

## 6. Migration stage + prerequisites

- Stage: in-tree preparation (this PREP). Destination repository does
  not exist yet; no standalone release; no deployment.
- Prerequisites to reuse: #6708 SDK publication, #6709 producer
  delegation, #6710 standalone template, #4047 gate implementation.
- Blockers: #8851 physical SDK relocation (Codex-owned; this drive
  consumes its published modules only), owner-designated QA EKS cutover
  rehearsal, independent review + stable exact-head CI at promotion.
- Mirror proof: `extraction_prep_mirror_test.go` in both packages
  (admission at 1.0.0, determinism, generation rebind, redaction
  sentinel, fail-closed hostile inputs, claim boundary, heartbeat
  guard, claim-ID namespace).
