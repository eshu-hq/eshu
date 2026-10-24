# Internal → Public Shim Map (`#8864`, analysis artifact)

This is a **design map, not an implementation**. It records how each in-tree
seam becomes a public SDK exchange at extraction, so the `#6710` template
repository and the `#4047` parity gate share one interpretation. Nothing here
changes runtime behavior.

## 1. Claim path: `ClaimedService` / `NextClaimed` → SDK `Result` / `Claim`

There is no unclaimed poll path: `ClaimedSource.NextClaimed`
(`source.go:86`) is the only emission entry. It binds a `workflow.WorkItem`
to one configured target by `scope_id` (`source.go:104-107`), stamps
`item.CurrentFencingToken`, and reuses `item.GenerationID` so retries are
idempotent.

| In-tree outcome | SDK `ResultState` (`sdk/go/collector/types.go:31-40`) | Host action |
| --- | --- | --- |
| envelopes committed | `complete` | validate facts, complete claim |
| target 403/401 → permission_hidden warning + rules committed | `partial` + `Status{warning}` + `observability.coverage_warning` facts | commit reachable evidence |
| target 404/405 → unsupported warning + rules committed | `partial` + `Status{warning}` + `observability.coverage_warning` facts | commit reachable evidence |
| rate-limited request counted, otherwise complete | `complete` with `Stats.RateLimits` surfaced as retry telemetry | complete, note throttling |
| resource_limit cut | `partial` + `Status{warning}` + truncated `observability.coverage_warning` | commit capped evidence |
| transient transport / retryable HTTP status | `retryable` | release claim, bounded retry |
| terminal (auth-denied 403 at claim scope, `bad_data` API status, contract violation) | `terminal` | terminal-fail claim |
| cancellation | (no result) | stop, no commit |

Duplicate delivery stays safe: stable keys are deterministic
(`stableFactKey`, `envelope.go:332`), and generation-scoped commit is
idempotent at the core boundary.

| In-tree field | SDK `Claim` (`sdk/go/collector/types.go:88-101`) |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorPrometheusMimir`) | `collector_kind` |
| `item.CollectorInstanceID` vs `SourceConfig.CollectorInstanceID` | `instance_id` (mismatch rejects, `source.go:90-95`) |
| `item.ScopeID` | `scope.id` |
| `item.GenerationID` | `generation.id` (reused on retry) |
| `target.FencingToken` / `CurrentFencingToken` | `fencing_token` |
| `item.Attempt`, deadline, `WorkItemID` | `attempt`, `deadline`, `work_item_id` |
| per-target scan config | `config_handle` (opaque; resolved host-side) |

Stale-claim rule (core-owned): the workflow control store, not the producer,
decides lease expiry and supersession. The producer must treat a claim it did
not `.Claim()` as foreign and never write for it.

## 2. The 4 `observability` kinds at `1.0.0` → SDK `FactDeclaration`

One declaration per kind, each `schema_versions: ["1.0.0"]`
(`facts/observability.go:89-108`):

```json
[
  {"kind": "observability.source_instance", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "observability.observed_target", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "observability.observed_rule", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "observability.coverage_warning", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]}
]
```

SDK `Fact` mapping (`types.go:124-135`): `stable_key` ← envelope stable
key, `source_confidence` ← `reported` above,
`source_ref.scope_id` ← target `scope_id`,
`source_ref.generation_id` ← committed generation, `payload` ← contract
payload merged by `mergeContractPayload` (`envelope.go:253`), `redactions`
← `_redacted` flags and `_fingerprint` fields with policy
`RedactionVersion` (`types.go:35`, `redaction.go:13-76`).

## 3. `TargetConfig` JSON (operator shape, unchanged by extraction)

`targetJSON` (`go/cmd/collector-prometheus-mimir/config.go:39-53`) keys:
`provider`, `scope_id`, `instance_id`, `base_url`, `path_prefix`,
`token_env`, `tenant_id`, `tenant_id_env`, `resource_limit`, `stale_after`,
`enabled`, `declared_ids`, `observed_only_hint`. Secrets travel by env
reference, never inline. Disabled targets are skipped before client
construction (`source.go:53`).

## 4. Telemetry the external producer must preserve

| Signal | Name | Emitted at |
| --- | --- | --- |
| span | `prometheus_mimir.observe` (`contract/observability/prometheus_mimir.go:10`) | `source.go:272` |
| span | `prometheus_mimir.fetch` (`contract/observability/prometheus_mimir.go:13`) | `source.go:281` |
| histogram | `eshu_dp_prometheus_mimir_fetch_duration_seconds` (`instruments.go:5694`) | `metrics.go:26` |
| counter | `eshu_dp_prometheus_mimir_provider_requests_total` (`instruments.go:3982`) | `metrics.go:24` |
| counter | `eshu_dp_prometheus_mimir_facts_emitted_total` (`instruments.go:3990`) | `metrics.go:54` |
| counter | `eshu_dp_prometheus_mimir_rate_limited_total` (`instruments.go:3998`) | `metrics.go:37` |
| counter | `eshu_dp_prometheus_mimir_retries_total` (`instruments.go:4006`) | `metrics.go:71` |
| counter | `eshu_dp_prometheus_mimir_redactions_total` (`instruments.go:4014`) | `metrics.go:61` |
| counter | `eshu_dp_prometheus_mimir_stale_total` (`instruments.go:4022`) | `metrics.go:80` |

Attributes stay `provider`, `status_class`, `fact_kind`, `reason`. Instance
IDs, target URLs, label values, raw PromQL, tenant IDs, tenant headers, and
token values must never appear in metric labels. The host additionally
records grant decisions per emission (`#6709`).

No-Observability-Change: this PREP adds no signals; the existing
`eshu_dp_prometheus_mimir_*` counters, fetch-duration histogram, and the two
spans already cover the claimed path. The external producer must preserve
them verbatim.

## 5. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary). External
   fails closed on missing/expired/revoked grant at install, activation, and
   every emission. See [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree calls `postgres.NewIngestionStore` directly
   (`service.go:42,72`). External returns facts; core commits.
3. **Claim control**: in-tree `ClaimedService` owns lease/heartbeat
   (`service.go:55-91`). External receives a bounded `Claim` and returns a
   `Result`; transitions stay `eshu-workflow`.
4. **Shared kinds**: the 4 kinds share the `observability` family with the
   grafana, loki, and tempo collectors. The grant's producer identity +
   scope (`prometheus_mimir`) is what separates authorities, not the kind
   names.
