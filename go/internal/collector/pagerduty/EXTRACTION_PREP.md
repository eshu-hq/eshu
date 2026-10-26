# PagerDuty Collector — Extraction Prep Census (`#8863`)

Status: **PREP ONLY**. No code moves, no deploy, no in-tree removal. This
manifest records the current in-tree truth so the cutover gate (`#4047`)
can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-pagerduty/` and
`go/internal/collector/pagerduty/`. Out of scope: `sdk/go` (`#8851`),
all other collectors, org repos, deployment values. The cutover plan
follows the Jira lane (`#8858`): one activation per in-tree scope, new
instance id, same scope id, in-tree producer disabled in the same values
change.

## 1. Current packages

### Binary: `go/cmd/collector-pagerduty/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:27,43,66` | entry point, `-mode=claimed-live\|cassette` | `parseArgs`, `run` |
| `config.go:36` | env config, `ESHU_COLLECTOR_INSTANCES_JSON` shape | `loadClaimedRuntimeConfig`, `selectPagerDutyInstance` |
| `config.go:105` | instance validation | `validatePagerDutyInstance` |
| `source_config.go:42` | claim-scoped source config load | `loadPagerDutySourceConfig`, `mapTarget` |
| `service.go:31` | credential-free replay wiring | `buildCassetteService` |
| `service.go:54` | claimed wiring | `buildClaimedService` |
| `service.go:90` | claim ID mint | `newClaimID` |
| `service.go:99` | runtime signal attach | `attachPagerDutyRuntimeSignals` |
| `doc.go` | package contract | — |
| `config_test.go` | env/instance selection tests | — |

No unclaimed poll mode exists: the binary only runs claimed-live or
cassette replay. Target secrets resolve exclusively through `token_env`
(`source_config.go:61`); raw tokens never appear in configuration.

### Library: `go/internal/collector/pagerduty/`

| File | Role | Key symbol |
| --- | --- | --- |
| `types.go:18` | provider name | `ProviderPagerDuty = "pagerduty"` |
| `types.go:40-54` | warning vocabulary | `ConfigWarningMissing` … `ConfigWarningTruncated` |
| `types.go:56-73` | failure classes | `FailureAuthDenied` … `FailureTerminal` |
| `types.go:76` | envelope identity | `EnvelopeContext` |
| `types.go:100,119,132` | normalized provider shapes | `Incident`, `LifecycleEvent`, `ChangeEvent` |
| `types.go:142,163,181` | live-config shapes | `ConfigService`, `ConfigIntegration`, `ConfigWarning` |
| `types.go:238,248` | claim-scoped config | `SourceConfig`, `TargetConfig` |
| `envelope.go:20,75,124` | 3 incident envelope constructors | `NewIncidentRecordEnvelope` … `NewChangeRecordEnvelope` |
| `config_envelope.go:22,96,182` | 3 config envelope constructors | `NewObservedPagerDutyServiceEnvelope` … `NewPagerDutyConfigCoverageWarningEnvelope` |
| `source.go:48` | claim-scoped source | `NewClaimedSource` |
| `source.go:89` | only emission entry | `(*ClaimedSource).NextClaimed` |
| `source.go:170` | failure classification | `classifiedProviderFailure` |
| `source.go:224` | envelope fan-out | `(*ClaimedSource).envelopes` |
| `client.go`, `http_config.go` | provider REST client | incident / log-entry / change-event / config reads |
| `pagination.go` | offset pagination | `paginateOffset` (local offset, never server-echoed) |
| `normalize.go:90,113,130` | provider JSON normalization | `normalizeIncidents` … `normalizeChangeEvents` |
| `redaction.go` | token/query-param stripping | URL redaction, routing-key fingerprint |
| `freshness.go` | collection window bounds | lookback/limit enforcement |
| `encode_helpers.go:12` | contract payload merge | `mergeContractPayload` |
| `doc.go` | package contract | — |

Every production file is listed. Failure mapping lives in one place
(`source.go:170`); pagination follows the provider `more` signal with a
locally computed next offset (`pagination.go`), and `Truncated` /
coverage warnings fire only when a configured bound stopped the fetch
while `more:true` remained — never on natural exhaustion.

## 2. Fact kinds and versions

All 6 kinds are `1.0.0` (`IncidentContextSchemaVersionV1` in
`go/internal/facts/incident/context.go`):

| Kind | Constructor | Producer record |
| --- | --- | --- |
| `incident.record` | `envelope.go:20` | `context.go:76` |
| `incident.lifecycle_event` | `envelope.go:75` | `context.go:76` |
| `change.record` | `envelope.go:124` | `context.go:76` |
| `incident_routing.coverage_warning` | `config_envelope.go:182` | `routing.go:87` |
| `incident_routing.observed_pagerduty_service` | `config_envelope.go:22` | `routing.go:87` |
| `incident_routing.observed_pagerduty_integration` | `config_envelope.go:96` | `routing.go:87` |

Both producer records are already cutover-shaped
(`AuthorityMode: core_issued_grant`,
`PlannedRepository: eshu-hq/eshu-collector-pagerduty`):
`context.go:76` binds `change.record`, `incident.lifecycle_event`,
`incident.record`; `routing.go:87` binds
`incident_routing.coverage_warning`,
`incident_routing.observed_pagerduty_integration`,
`incident_routing.observed_pagerduty_service`
(declared a producer of the coverage warning by `#8926`).

Shared-kind note: `change.record` is a cross-provider family kind. The
producer grant binds identity + kind + schema + scope, so the extracted
`pagerduty` producer cannot emit under a sibling's authority; see
[PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).

## 3. Consumers (must agree at cutover)

- Canonical projection: `go/internal/projector/incident/routing/`.
- Reducer domain: `incident_routing_materialization`
  (fact registry `ReducerDomain`, truth profile `provider_gated`).
- Query/read: `GET /api/v0/incidents/{incident_id}/context`
  (fact registry `ReadSurface`).
- Coordinator: `go/internal/coordinator/pagerduty_service.go` plans claim
  rows per active instance; stays core-owned.
- Entrypoint contract:
  `go/internal/collector/entrypoints/collector_entrypoints.yaml:3-38`
  (`collector-pagerduty`, `scope_kind: incident`, `token_env` auth,
  `pagerduty.NewClaimedSource`).
- The parity pack in `testdata/8863-parity-pagerduty.json` is the cutover
  comparison input (`source_parity_8863_test.go`, AC3, in-tree side).

## 4. Durable-state owner

The collector owns **no** durable state. Durable writes stay core-owned:

- Fact commit: `postgres.NewIngestionStore` (via `service.go:54` claimed
  wiring).
- Claim lifecycle: `collector.ClaimedService`.
- In-tree transient state only: per-call `CollectionResult` /
  `ConfigCollectionResult` (`types.go:195,207`); both reset per claim and
  never persist. `AllowEmptySuccessor` (`types.go:248`) is an explicit
  operator attestation stamped onto the generation, not stored state.

Admission, persistence, reducer queue, projection, and retraction remain
Eshu core per the extraction policy. The external producer must submit a
bounded result; it never receives Postgres, graph, queue, lease, reducer,
API, MCP, or workflow handles.

## 5. Protocols

- PagerDuty REST: incident list, incident log entries, related change
  events, services, service integrations — all offset-paginated through
  `paginateOffset`, honoring `PaginationMaxPages` / `PaginationMaxRecords`.
- Auth: `Authorization: Token` from `token_env`; facts keep a SHA-256
  fingerprint of routing keys, never raw values (`config_envelope.go:298`).
- Target wire shape: `ESHU_COLLECTOR_INSTANCES_JSON` instance
  `Configuration.targets[]`
  (`go/cmd/collector-pagerduty/source_config.go:20-45`); runtime shape
  `TargetConfig` (`types.go:248`). Secrets travel by env reference, never
  inline.
- Public target contract: `collector-sdk/v1alpha1`.
- Cassette replay: binary flag `-mode=cassette -cassette-file=<path>`
  (`main.go:44-64`, `service.go:31`).
- Bounded retries with `Stats.RateLimits` / `Stats.Retries` accounting;
  generation lag observed via `recordGenerationLag` (`source.go:401`).

## 6. Artifacts

- Today: single in-tree binary `collector-pagerduty`
  (`runtimeName` in `main.go:27`). No published artifact from this PREP.
- Released: `eshu-hq/eshu-collector-pagerduty` `v0.1.0` (tag dereferences
  to `c074c0bd`, merge of satellite PR `#1`).
- Reconcile-before-cutover: the satellite `go.mod` still requires the old
  `github.com/eshu-hq/eshu/sdk/go/{collector,factschema} v0.2.0` path.
  Cutover builds against the supported `github.com/eshu-hq/eshu-sdk`
  path at the exact published `0.1.0` candidates
  (`collector v0.1.0-rc.3`, `factschema v0.1.0-rc.5`) — never `latest`,
  no workspace replaces.
- At cutover: digest-pinned OCI artifact executed via the
  component-extension host `process` runner with the seeded binary and
  manifest. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` moving toward `ready_for_cutover`: repository and `v0.1.0`
release exist; remaining gates are the SDK-path reconcile, parity proof
(§3), producer delegation (`#6709`), activation template (`#6710`,
`#8894`), rollback rehearsal (`#8895`), and the per-collector cutover
gate (`#4047`). Roadmap owners: `#6707` inventory, `#6708`
SDK/fact-schema, `#6709` producer delegation, `#6710` template and
compatibility harness, `#4047` per-collector cutover gate.

## 8. Prerequisites for cutover

1. Satellite `go.mod` reconciled to the supported SDK path at the exact
   pins above (satellite cutover PR).
2. First-party producer delegation live (`#6709`); grant drafts recorded
   in [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
3. Activation entry from the `#6710`/`#8894` template with the
   compatibility harness matrix (newest collector × oldest core and
   reverse).
4. Parity proof on representative inputs via the fixture pack in
   `testdata/8863-parity-pagerduty.json` and
   `source_parity_8863_test.go` (AC3, in-tree side).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)). Cutover order: Jira (`#8858`)
   first, then PagerDuty — new instance id
   (`pagerduty-cx-<environment>`), same scope id
   (`pagerduty:account:<account>`), in-tree producer disabled in the same
   values change.
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)).
