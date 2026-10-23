# Internal → Public Shim Map (`#8861`, analysis artifact)

This is a **design map, not an implementation**. It records how each in-tree
seam becomes a public SDK exchange at extraction, so the `#6710` template
repository and the `#4047` parity gate share one interpretation. Nothing here
changes runtime behavior.

## 1. Poll path: `collector.Service.Next` → SDK `Result`

In-tree: `Source.Next` (`runtime/source.go:65`) yields one
`collector.CollectedGeneration` (`go/internal/collector/service.go:32-54`)
per target; `Service.Run` (`service.go:198`) commits it through the
`Committer` boundary (`service.go:99-107`).

| In-tree outcome | SDK `ResultState` (`sdk/go/collector/types.go:31-41`) | Host action |
| --- | --- | --- |
| envelopes committed | `complete` | validate facts, complete claim |
| `ok=false`, batch exhausted | `complete` with zero facts, or `unchanged` | complete, no writes |
| transient transport / retryable HTTP status (`runtime/transport.go`) | `retryable` | release claim, bounded retry |
| terminal (auth, terminal HTTP, contract violation) | `terminal` | terminal-fail claim |
| referrers unsupported, tag window cut | `partial` + `Status{warning}` + `oci_registry.warning` facts | commit reachable evidence |
| cancellation | (no result) | stop, no commit |

Duplicate delivery stays safe: stable keys are deterministic
(`identity.go:17-61`), and generation-scoped commit is idempotent at the
core boundary.

## 2. Claim path: `ClaimedService` / `NextClaimed` → SDK `Claim`

In-tree: `ClaimedSource.NextClaimed` (`runtime/claimed_source.go:25`) binds a
`workflow.WorkItem` to one configured target by `scope_id`
(`claimed_source.go:36-55`), stamps `item.CurrentFencingToken`, and reuses
`item.GenerationID` so retries are idempotent.

| In-tree field | SDK `Claim` (`sdk/go/collector/types.go:87-101`) |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorOCIRegistry`) | `collector_kind` |
| `item.CollectorInstanceID` vs `Config.CollectorInstanceID` | `instance_id` (mismatch rejects, `claimed_source.go:41-44`) |
| `item.ScopeID` | `scope.id` |
| `item.GenerationID` | `generation.id` (reused on retry) |
| `target.FencingToken` / `CurrentFencingToken` | `fencing_token` |
| `item.Attempt`, deadline, `WorkItemID` | `attempt`, `deadline`, `work_item_id` |
| per-target scan config | `config_handle` (opaque; resolved host-side) |

Stale-claim rule (core-owned): the workflow control store, not the producer,
decides lease expiry and supersession. The producer must treat a claim it did
not `.Claim()` as foreign and never write for it.

## 3. The 7 `oci_registry` kinds at `1.0.0` → SDK `FactDeclaration`

One declaration per kind, each `schema_versions: ["1.0.0"]`
(`chain/oci_registry.go:30-43`):

```json
[
  {"kind": "oci_registry.repository", "schema_versions": ["1.0.0"],
   "source_confidence": ["observed", "reported"]},
  {"kind": "oci_registry.image_tag_observation", "schema_versions": ["1.0.0"],
   "source_confidence": ["observed"]},
  {"kind": "oci_registry.image_manifest", "schema_versions": ["1.0.0"],
   "source_confidence": ["observed"]},
  {"kind": "oci_registry.image_index", "schema_versions": ["1.0.0"],
   "source_confidence": ["observed"]},
  {"kind": "oci_registry.image_descriptor", "schema_versions": ["1.0.0"],
   "source_confidence": ["observed"]},
  {"kind": "oci_registry.image_referrer", "schema_versions": ["1.0.0"],
   "source_confidence": ["observed", "reported"]},
  {"kind": "oci_registry.warning", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]}
]
```

SDK `Fact` mapping (`types.go:124-135`): `stable_key` ← envelope stable key,
`source_confidence` ← observed/reported above, `source_ref.scope_id` ←
repository `scope_id`, `source_ref.generation_id` ← committed generation,
`payload` ← contract payload merged by `mergeContractPayload`
(`encode_helpers.go`), `redactions` ← annotation allowlist redactions
(`types.go:74`, `RedactedValue`).

## 4. `TargetConfig` JSON (operator shape, unchanged by extraction)

`targetJSON` (`go/cmd/collector-oci-registry/config.go:27-60`) keys:
`provider`, `registry`, `base_url`, `repository_key`, `repository`,
`region`, `registry_id`, `registry_host`, `references`, `tag_limit`,
`visibility`, `auth_mode`, `source_uri`, `username_env`, `password_env`,
`bearer_token_env`, `aws_profile`, `fencing_token`,
`allow_empty_successor`. Secrets travel by env reference, never inline.
Empty-successor attestation maps to `Generation.trusted_deletion`
(`types.go:114-121`; in-tree stamp `runtime/source.go:468`).

## 5. Telemetry the external producer must preserve

| Signal | Name | Emitted at |
| --- | --- | --- |
| span | `oci_registry.scan` (`telemetry/contract.go:709`) | `runtime/source.go:102` |
| span | `oci_registry.api_call` (`contract.go:710`) | `recordAPICall`, `source.go:485` |
| histogram | `eshu_dp_oci_registry_scan_duration_seconds` (`instruments.go:5417`) | `source.go:114` |
| counter | `eshu_dp_oci_registry_tags_observed_total` (`instruments.go:3555`) | tag listing |
| counter | `eshu_dp_oci_registry_manifests_observed_total` (`instruments.go:3563`) | `source.go:218` |
| counter | `eshu_dp_oci_registry_referrers_observed_total` (`instruments.go:3571`) | `source.go:400` |
| counter | `eshu_dp_oci_registry_api_calls_total` (`instruments.go:3547`) | `source.go:486` |

Attributes stay `provider`, `result`, `tls_mode`, media/artifact family. The
host additionally records grant decisions per emission (`#6709`).

## 6. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary). External
   fails closed on missing/expired/revoked grant at install, activation, and
   every emission. See [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree calls `postgres.NewIngestionStore` directly
   (`service.go:54,80,110`). External returns facts; core commits.
3. **Claim control**: in-tree `ClaimedService` owns lease/heartbeat
   (`claimed_service.go:59`). External receives a bounded `Claim` and returns
   a `Result`; transitions stay `eshu-workflow`.
