# Internal → Public Shim Map (`#8860`, analysis artifact)

This is a **design map, not an implementation**. It records how each in-tree
seam becomes a public SDK exchange at extraction, so the `#6710` template
repository and the `#4047` parity gate share one interpretation. Nothing here
changes runtime behavior.

## 1. Claim path: `ClaimedService` / `NextClaimed` → SDK `Result` / `Claim`

There is no unclaimed poll mode: `ClaimedSource.NextClaimed`
(`source.go:85`) is the only emission entry. It binds a `workflow.WorkItem`
to one configured target by `scope_id` (`source.go:102-106`), stamps
`item.CurrentFencingToken`, and reuses `item.GenerationID` so retries are
idempotent.

| In-tree outcome | SDK `ResultState` (`sdk/go/collector/types.go:31-40`) | Host action |
| --- | --- | --- |
| envelopes committed | `complete` | validate facts, complete claim |
| target 403/401 → permission_hidden warning path | `partial` + `Status{warning}` + `observability.coverage_warning` facts | commit reachable evidence |
| target 404/410 → unsupported warning path | `partial` + `Status{warning}` + `observability.coverage_warning` facts | commit reachable evidence |
| rate-limited request counted, otherwise complete | `complete` with `Stats.RateLimits` surfaced as retry telemetry | complete, note throttling |
| resource_limit / label-value cap cut | `partial` + `Status{warning}` + truncated/high-cardinality `observability.coverage_warning` | commit capped evidence |
| transient transport / retryable HTTP status | `retryable` | release claim, bounded retry |
| terminal (`bad_data` API status, contract violation, not-found scope) | `terminal` | terminal-fail claim |
| cancellation | (no result) | stop, no commit |

Duplicate delivery stays safe: stable keys are deterministic
(`stableFactKey`, `envelope.go:329`), and generation-scoped commit is
idempotent at the core boundary.

| In-tree field | SDK `Claim` (`sdk/go/collector/types.go:88-101`) |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorLoki`) | `collector_kind` |
| `item.CollectorInstanceID` vs `SourceConfig.CollectorInstanceID` | `instance_id` (mismatch rejects, `source.go:89-94`) |
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
(`facts/observability.go:88-110`):

```json
[
  {"kind": "observability.source_instance", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "observability.observed_log_signal", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "observability.observed_rule", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "observability.coverage_warning", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]}
]
```

SDK `Fact` mapping (`sdk/go/collector/types.go:124-135`): `stable_key` ← envelope stable
key, `source_confidence` ← `reported` above,
`source_ref.scope_id` ← target `scope_id`,
`source_ref.generation_id` ← committed generation, `payload` ← contract
payload merged by `mergeContractPayload` (`envelope.go:250`), `redactions`
← `_redacted` flags and `_fingerprint` fields with policy
`RedactionVersion` (`types.go:31`, `redaction.go`).

## 3. `TargetConfig` JSON (operator shape, unchanged by extraction)

`targetJSON` (`go/cmd/collector-loki/config.go:35-55`) keys:
`scope_id`, `instance_id`, `base_url`, `path_prefix`, `token_env`,
`tenant_id`, `tenant_id_env`, `resource_limit`, `label_value_names`,
`max_label_values_per_label`, `series_matchers`, `series_lookback`,
`stale_after`, `enabled`, `declared_ids`, `observed_only_hint`. The token travels by env reference only
(`token_env`); tenant identity may travel inline (`tenant_id`) or by env
reference (`tenant_id_env`). Disabled targets are skipped before client
construction (`source.go:52`).

## 4. Telemetry the external producer must preserve

| Signal | Name | Emitted at |
| --- | --- | --- |
| span | `loki.observe` (`telemetry/contract/observability/loki.go:9`) | `source.go:279` |
| span | `loki.fetch` (`telemetry/contract/observability/loki.go:11`) | `source.go:288` |
| histogram | `eshu_dp_loki_fetch_duration_seconds` (`instruments.go`) | `metrics.go:26` |
| counter | `eshu_dp_loki_provider_requests_total` (`instruments.go:706`) | `metrics.go:25` |
| counter | `eshu_dp_loki_facts_emitted_total` (`instruments.go:707`) | `metrics.go:54` |
| counter | `eshu_dp_loki_rate_limited_total` (`instruments.go:708`) | `metrics.go:40` |
| counter | `eshu_dp_loki_retries_total` (`instruments.go:709`) | `metrics.go:75` |
| counter | `eshu_dp_loki_redactions_total` | `metrics.go:65` |
| counter | `eshu_dp_loki_stale_total` | `metrics.go:84` |
| counter | `eshu_dp_loki_high_cardinality_rejected_total` | `metrics.go:94` |

Attributes stay `provider`, `status_class`, `fact_kind`, `reason`. Instance
IDs, target URLs, label values, raw LogQL, tenant IDs, tenant headers, and
token values must never appear in metric labels. The host additionally
records grant decisions per emission (`#6709`).

No-Observability-Change: this PREP adds no signals; the existing
`eshu_dp_loki_*` counters, fetch-duration histogram, and the two spans
already cover the claimed path. The external producer must preserve
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
   grafana, prometheus-mimir, and tempo collectors (loki uniquely owns
   `observed_log_signal`). The grant's producer identity + scope (`loki`)
   is what separates authorities, not the kind names.
