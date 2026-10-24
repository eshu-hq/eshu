# Internal → Public Shim Map (`#8856`, analysis artifact)

This is a **design map, not an implementation**. It records how each in-tree
seam becomes a public SDK exchange at extraction, so the `#6710` template
repository and the `#4047` parity gate share one interpretation. Nothing here
changes runtime behavior.

Pinned SDK: `github.com/eshu-hq/eshu-sdk/collector v0.1.0-rc.1` and
`github.com/eshu-hq/eshu-sdk/factschema v0.1.0-rc.1` (immutable,
proxy-verified per `#8851`; newer rc tags lack source-bound approval — do
not use). Verified against this tree: `collector/types.go` and
`validation.go` are byte-identical to the in-tree SDK; `factschema`
GCP `Encode*` files differ by import path only
(`eshu-hq/eshu-sdk/...` vs `eshu-hq/eshu/sdk/go/...`); `collector/schema.go`
adds the `Request`/`command`/`transport`/`entrypoints` surface absent
in-tree. No GCP-needed symbol is missing from RC1 — see §7.

## 1. Poll path: `collector.Service.Next` → SDK `Result`

In-tree: `Source.Next` (`gcpruntime/source.go:62`) yields one
`collector.CollectedGeneration` (the `CollectedGeneration` type in
`go/internal/collector/service.go`) per scope; `Service.Run`
(`service.go:191`) commits it through the `Committer` boundary
(`service.go:99-107`).

| In-tree outcome | SDK `ResultState` (`collector@v0.1.0-rc.1/types.go:30-41`) | Host action |
| --- | --- | --- |
| envelopes committed | `complete` | validate facts, complete claim |
| `ok=false`, batch exhausted | `complete` with zero facts, or `unchanged` | complete, no writes |
| hard fetch error (non-warning transport failure, `source.go:169`) | `retryable` | release claim, bounded retry |
| contract violation (bad config, zero redaction key, tag provider absent) | `terminal` | terminal-fail claim |
| `ProviderWarning` / dangling token → `gcp_collection_warning` (`source.go:157-167,192-200`) | `partial` + `Status{warning}` + `gcp_collection_warning` facts | commit reachable evidence |
| stale fencing token (`source.go:100-105`) | `partial` + single stale `gcp_collection_warning` | commit warning, no resource writes |
| cancellation (`ctx.Err`, `source.go:151`) | (no result) | stop, no commit |

Duplicate delivery stays safe: resource keys are deterministic
(`generation.go:416-422` full-name + asset-type dedupe; envelope stable keys
in `envelope.go:49`, `relationship.go`, `tag_observation.go`,
`iam_policy_observation.go`, `dns_record.go`, `image_reference.go`), and
generation-scoped commit is idempotent at the core boundary.

**Delta vs OCI lane:** the GCP path has no empty-successor attestation
(no `TrustedDeletion` input anywhere in `gcpcloud/` or the binary). An
emptied scope emits whatever the pages contain; the host
successor-emptiness backstop applies unchanged to external results.

## 2. Claim path: `ClaimedService` / `NextClaimed` → SDK `Claim`

In-tree: `Source.NextClaimed` (`gcpruntime/claimed_source.go:20`) binds a
`workflow.WorkItem` to one configured scope by `scope_id`
(`claimed_source.go:75-87`), stamps `item.CurrentFencingToken`, and reuses
`item.GenerationID` (= `item.SourceRunID`, enforced at
`claimed_source.go:67-73`) so retries are idempotent.

| In-tree field | SDK `Claim` (`collector@v0.1.0-rc.1/types.go:88-101`) |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorGCP`) | `collector_kind` |
| `item.SourceSystem` (`scope.CollectorGCP`) | `source_system` |
| `item.CollectorInstanceID` vs `Config.CollectorInstanceID` | `instance_id` (mismatch rejects, `claimed_source.go:51-53`) |
| `item.ScopeID` | `scope.id` |
| `item.GenerationID` (= `SourceRunID`) | `generation.id` (reused on retry) |
| `item.CurrentFencingToken` (int64, must be positive) | `fencing_token` (**string** — shim stringifies the int64) |
| `item.Attempt`, deadline, `WorkItemID` | `attempt`, `deadline`, `work_item_id` |
| per-scope scan config | `config_handle` (opaque; resolved host-side) |

Stale-claim rule (core-owned): the workflow control store, not the producer,
decides lease expiry and supersession. The producer must treat a claim it did
not `.Claim()` as foreign and never write for it.

## 3. The 7 `gcp` kinds at `1.1.0`/`1.0.0` + 3 mirror kinds → SDK `FactDeclaration`

One declaration per kind; `gcp_cloud_resource` at `1.1.0`, all others at
`1.0.0` (`cloud/gcp.go:73-81`, `identity/iam.go`):

```json
[
  {"kind": "gcp_cloud_resource", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_cloud_relationship", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_tag_observation", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_iam_policy_observation", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_dns_record", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_image_reference", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_collection_warning", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_iam_principal", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_iam_permission_policy", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "gcp_iam_trust_policy", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]}
]
```

SDK `Fact` mapping (`collector@v0.1.0-rc.1/types.go:125-135`):
`stable_key` ← envelope stable key, `source_confidence` ← reported,
`source_ref.scope_id` ← scope, `source_ref.generation_id` ← committed
generation, `payload` ← `factschema.EncodeGCP*` / `EncodeGCP IAM*` output
(`factschema@v0.1.0-rc.1/decode_gcp.go`, `decode_secretsiam_source.go`),
`redactions` ← annotation allowlist redactions.

**Manifest-shape question for `#8851`/`#6709` (no SDK code change):** mirror
envelopes stamp `collector_kind = "secrets_iam_posture"`, and their
producer record names `secrets-iam-posture`. The extracted component
manifest must either declare both families or reconcile the kind to the
`gcp-cloud` producer before cutover. Pinned, not fixed, by
`source_parity_8856_test.go`.

## 4. Scope config (operator shape, unchanged by extraction)

`ScopeConfig` (`gcpruntime/config.go:34-69`) keys:
`scope_id` (derived `gcp:<parent_kind>:<parent_id>:<asset_family>:<content_family>:<location_bucket>`
when empty), `parent_scope_kind` (`organization`|`folder`|`project`),
`parent_scope_id`, `asset_type_family` (default `mixed`),
`content_family` (default `resource`), `location_bucket` (default `global`),
`generation_id` (claim-supplied on the claimed path), `fencing_token`
(positive int64), `credential_ref` (name only — secrets travel by env
reference, never inline), `direct_tags_enabled`, `effective_tags_enabled`.
Poll default `30m` (`catalog.DefaultPollInterval`) when unset.

## 5. Telemetry the external producer must preserve

Collector instruments (`gcpcloud/metrics.go:43-99`):

| Signal | Name | Emitted at |
| --- | --- | --- |
| counter | `eshu_dp_gcp_cloud_claims_total` | claim lifecycle by `started/succeeded/partial/failed` |
| counter | `eshu_dp_gcp_cloud_api_calls_total` | CAI/tag fetch by operation, scope kind, family, status class |
| counter | `eshu_dp_gcp_cloud_pages_total` | page fetch by parent scope kind |
| counter | `eshu_dp_gcp_cloud_page_token_resumes_total` | continuation resume |
| counter | `eshu_dp_gcp_cloud_facts_emitted_total` | emission by fact kind + parent scope kind |
| counter | `eshu_dp_gcp_cloud_warnings_total` | warning by kind + outcome |
| counter | `eshu_dp_gcp_cloud_attribute_extractions_total` | typed-depth extraction by asset family + outcome |
| histogram | `eshu_dp_gcp_cloud_freshness_lag_seconds` | provider→Eshu lag (explicit buckets per #7084) |

Collector spans reuse `collector.observe` / `collector.stream`
(`telemetry/contract.go:359-360`); reducer-side
`reducer.gcp_relationship_materialization` stays core-owned. Labels stay
bounded enums only — never resource names, project IDs, labels, IAM
members, DNS names, image references, URLs, or credential names. The host
additionally records grant decisions per emission (`#6709`).

## 6. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary). External
   fails closed on missing/expired/revoked grant at install, activation, and
   every emission. See [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree calls `postgres.NewIngestionStore` directly
   (`service.go:62,98`). External returns facts; core commits.
3. **Claim control**: in-tree `ClaimedService` owns lease/heartbeat.
   External receives a bounded `Claim` and returns a `Result`; transitions
   stay `eshu-workflow`.
4. **Credential resolution**: in-tree resolves `credential_ref` out of band
   into `LiveClient`. External receives only the opaque `config_handle`;
   raw credential material never crosses the boundary in either direction.
5. **Fencing token wire type**: int64 in-tree vs string in SDK `Claim` —
   shim stringifies; comparison stays numeric host-side.

## 7. SDK gap review vs RC1 (handoff note for `#8851`)

Checked 2026-10-24 against the proxy-verified RC1 module zips. **No GCP
symbol is missing**: `Result`/`Claim`/`Generation`/`Fact`/`SourceRef`/
`Status`/`Contract`/`FactDeclaration`, `SourceConfidenceReported`,
`ResultComplete/Unchanged/Partial/Retryable/Terminal`,
`ValidateResult`, `Request.Validate`, all 7 `EncodeGCP*` + 3
`EncodeGCPIAM*` encoders, and the `gcp/v1` + `secretsiam/v1` payload types
are present and behavior-identical to this tree (import-path-only diff).

One manifest-shape question, no code change: §3 mirror-kind family
membership. Do **not** edit `sdk/go` — Codex `#8851` lane owns it.
