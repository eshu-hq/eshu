# Internal → Public Shim Map (`#8863`, analysis artifact)

This is a **design map, not an implementation**. It records how each
in-tree seam becomes a public SDK exchange at extraction, so the
`#6710` template repository and the `#4047` parity gate share one
interpretation. Nothing here changes runtime behavior.

## 1. Claim path: `ClaimedService` / `NextClaimed` → SDK `Result` / `Claim`

There is no unclaimed poll path: `ClaimedSource.NextClaimed`
(`source.go:89`) is the only emission entry. It binds a
`workflow.WorkItem` to one configured target by `scope_id`, stamps the
current fencing token, and reuses `item.GenerationID` so retries are
idempotent.

| In-tree outcome | SDK `ResultState` | Host action |
| --- | --- | --- |
| envelopes committed | `complete` | validate facts, complete claim |
| claim-level list fetch fails (incidents, log entries): 401/403 → auth-denied, 404 → not-found, both `Terminal:true` (`client.go`, `ClassifyStatus`) | `terminal` | terminal-fail claim, no envelopes |
| claim-level list fetch fails transiently (transport, 5xx, timeout) | `retryable` | release claim, bounded retry |
| rate-limited request counted, otherwise complete | `complete` with `Stats.RateLimits` surfaced as retry telemetry | complete, note throttling |
| per-resource fetch fails (related change events via `configWarningFromError`, config resources): 401/403 → `permission_hidden`, 404 → `missing`, other 4xx → `unsupported`, 429 → rate-limited warning | `partial` + `Status{warning}` + `incident_routing.coverage_warning` facts | commit reachable evidence |
| pagination bound hit while `more:true` → truncated warning | `partial` + `Status{warning}` + truncated `incident_routing.coverage_warning` | commit capped evidence |
| terminal (`bad_data` API status, contract violation, non-HTTP terminal failure) | `terminal` | terminal-fail claim |
| cancellation | (no result) | stop, no commit |

Duplicate delivery stays safe: stable keys are deterministic
(`providerStableKey`, `envelope.go:198`), and generation-scoped commit
is idempotent at the core boundary. `AllowEmptySuccessor`
(`types.go:248`) travels as an explicit operator attestation on the
generation, never as producer-stored state.

| In-tree field | SDK `Claim` |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorPagerDuty`) | `collector_kind` |
| `item.CollectorInstanceID` vs `SourceConfig.CollectorInstanceID` | `instance_id` (mismatch rejects) |
| `item.ScopeID` | `scope.id` |
| `item.GenerationID` | `generation.id` (reused on retry) |
| target fencing token / `CurrentFencingToken` | `fencing_token` |
| `item.Attempt`, deadline, `WorkItemID` | `attempt`, `deadline`, `work_item_id` |
| per-target scan config | `config_handle` (opaque; resolved host-side) |

Stale-claim rule (core-owned): the workflow control store, not the
producer, decides lease expiry and supersession. The producer must treat
a claim it did not `.Claim()` as foreign and never write for it.

## 2. The 6 `incident` / `incident_routing` kinds at `1.0.0` → SDK `FactDeclaration`

One declaration per kind, each `schema_versions: ["1.0.0"]`
(producer records in `go/internal/facts/incident/context.go` and
`routing.go`):

```json
[
  {"kind": "incident.record", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "incident.lifecycle_event", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "change.record", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "incident_routing.coverage_warning", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "incident_routing.observed_pagerduty_service", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "incident_routing.observed_pagerduty_integration", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]}
]
```

SDK `Fact` mapping: `stable_key` ← envelope stable key,
`source_confidence` ← `reported` above,
`source_ref.scope_id` ← target `scope_id`,
`source_ref.generation_id` ← committed generation, `payload` ← contract
payload merged by `mergeContractPayload` (`encode_helpers.go:12`),
`redactions` ← token-stripped URLs and routing-key fingerprints
(`redaction.go`, `config_envelope.go:298`).

## 3. `TargetConfig` JSON (operator shape, unchanged by extraction)

`targetJSON` (`go/cmd/collector-pagerduty/source_config.go:20-40`) keys:
`provider`, `scope_id`, `account_id`, `token_env`, `api_base_url`,
`source_uri`, `incident_limit`, `incident_lookback`, `log_entry_limit`,
`change_event_limit`, `allowed_service_ids`,
`config_validation_enabled`, `config_resource_limit`,
`pagination_max_pages`, `pagination_max_records`,
`allow_empty_successor`. Secrets travel by env reference, never inline.
Targets without a resolvable token are rejected before client
construction (`validateTarget`, `source.go:174`).

## 4. Telemetry the external producer must preserve

| Signal | Name | Emitted at |
| --- | --- | --- |
| span | `pagerduty.observe` (`contract/thirdparty/pagerduty.go:9`) | `source.go:352` |
| span | `pagerduty.fetch` (`contract/thirdparty/pagerduty.go`) | `source.go:361` |
| counter | `eshu_dp_pagerduty_provider_requests_total` (`PagerDutyProviderRequests`) | `source.go:372` |
| histogram | `eshu_dp_pagerduty_fetch_duration_seconds` (`PagerDutyFetchDuration`) | `source.go:373` |
| counter | `eshu_dp_pagerduty_rate_limited_total` (`PagerDutyRateLimited`) | `source.go:380` |
| counter | `eshu_dp_pagerduty_facts_emitted_total` (`PagerDutyFactsEmitted`) | `source.go:394` |
| histogram | `eshu_dp_pagerduty_generation_lag_seconds` (`PagerDutyGenerationLag`) | `source.go:409` |
| counter | `eshu_dp_pagerduty_config_redactions_total` (`PagerDutyConfigRedactions`) | `source_config.go:70` |
| counter | `eshu_dp_pagerduty_config_resources_observed_total` (`PagerDutyConfigResourcesObserved`) | `source_config.go:97` |
| counter | `eshu_dp_pagerduty_config_drift_candidates_total` (`PagerDutyConfigDriftCandidates`) | `source_config.go:103` |
| counter | `eshu_dp_pagerduty_config_partial_failures_total` (`PagerDutyConfigPartialFailures`) | `source_config.go:127` |

Attributes stay `provider`, `status_class`, `fact_kind`. Incident IDs,
titles, service names, integration names, escalation-policy names,
URLs, routing keys, and token environment names must never appear in
metric labels. The host additionally records grant decisions per
emission (`#6709`).

No-Observability-Change: this PREP adds no signals; the existing
seven `eshu_dp_pagerduty_*` counters, the two histograms, and the two
spans already cover the claimed and config-validation paths. The
external producer must preserve them verbatim.

## 5. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary).
   External fails closed on missing/expired/revoked grant at install,
   activation, and every emission. See
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree commits through the claimed-service
   ingestion store (`service.go:54`). External returns facts; core
   commits.
3. **Claim control**: in-tree `ClaimedService` owns lease/heartbeat.
   External receives a bounded `Claim` and returns a `Result`;
   transitions stay `eshu-workflow`.
4. **Shared kinds**: `change.record` is shared with other provider
   collectors. The grant's producer identity + scope (`pagerduty`) is
   what separates authorities, not the kind names.
5. **FactID construction**: the host builds the envelope, so `FactID`
   values change by construction. Parity is judged on kind, stable key,
   payload, scope, and generation first, then on reducer, API, and MCP
   read truth (arbiter ruling on `#8863`).
