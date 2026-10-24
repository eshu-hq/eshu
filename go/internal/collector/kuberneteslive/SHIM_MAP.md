# Internal → Public Shim Map (`#8859`, analysis artifact)

This is a **design map, not an implementation**. It records how each in-tree
seam becomes a public SDK exchange at extraction, so the `#6710` template
repository and the `#4047` parity gate share one interpretation. Nothing here
changes runtime behavior.

Pinned SDK surface: `github.com/eshu-hq/eshu-sdk/collector v0.1.0-rc.1` and
`github.com/eshu-hq/eshu-sdk/factschema v0.1.0-rc.1` (immutable,
proxy-verified per `#8851`; newer rc tags lack source-bound approval — do not
use). Verified against the module cache during this PREP: `collector/types.go`
is byte-identical to in-tree `sdk/go/collector/types.go`, and every
`factschema` encode the lane uses (`EncodeKubernetesLivePodTemplate`,
`EncodeKubernetesLiveRelationship`, `EncodeKubernetesLiveWarning`,
`EncodeKubernetesLiveNamespace`, plus the `secretsiam` encodes) is identical
modulo the expected `eshu-sdk` vs in-tree import path. **No SDK symbol
missing: no handoff on `#8851` is needed for this lane.** Do not edit `sdk/go`
from this lane.

## 1. Poll path: `collector.Service` + `Source.Next` → SDK `Result`

In-tree: `Source.Next` (`source.go:76`) yields one
`collector.CollectedGeneration` per cluster; `Service.Run` commits it through
the `Committer` boundary (`service.go:59-66`).

| In-tree outcome | SDK `ResultState` (`collector/types.go:31-41`) | Host action |
| --- | --- | --- |
| envelopes committed | `complete` | validate facts, complete claim |
| `ok=false`, batch exhausted | `complete` with zero facts, or `unchanged` | complete, no writes |
| hard error (ping failure, missing auth, invalid config) | `terminal` | terminal-fail claim |
| forbidden/partial family list | `partial` + `Status{warning}` + `kubernetes_live.warning` facts | commit reachable evidence |
| invalid owner reference | `partial` + `Status{warning}` + ambiguity warning fact | commit, no invented identity |
| cancellation | (no result) | stop, no commit |

There is no in-tree retryable/terminal taxonomy for list failures (no
`transport.go` analog): partial is in-band evidence, hard errors surface at
the next poll. The shim must define the retryable set (e.g. transient API
server errors) that the external producer maps to `retryable`.

Duplicate delivery stays safe: stable keys are deterministic
(`identity.go:63-73`), generation IDs re-derive identically at a fixed instant
(pinned by `TestKubernetesLive8859ParityDuplicateDeliveryIsStable`), and
generation-scoped commit is idempotent at the core boundary.

## 2. Claim path: UNIMPLEMENTED in-tree → SDK `Claim` (designed, not pinned)

Claim-driven collection is deferred: no `ClaimedService` wiring exists in
`go/cmd/collector-kubernetes-live`. The mapping below is the design the
`#6710` repository must implement; there is no in-tree behavior to pin.

| Prospective in-tree field | SDK `Claim` (`collector/types.go:88-101`) |
| --- | --- |
| `scope.CollectorKubernetesLive` | `collector_kind` |
| `Config.CollectorInstanceID` | `instance_id` |
| cluster scope ID (`ClusterScopeID`) | `scope.id` |
| `clusterGenerationID` (reused on retry) | `generation.id` |
| `ClusterTarget.FencingToken` (int64) | `fencing_token` (string; format at the seam) |
| per-cluster target + auth | `config_handle` (opaque; resolved host-side) |
| — (no in-tree analog) | `attempt`, `deadline`, `work_item_id` |

Stale-claim rule (core-owned): the workflow control store, not the producer,
decides lease expiry and supersession. The producer must treat a claim it did
not `.Claim()` as foreign and never write for it. When a scheduler for this
kind lands, it must use the shared `inTreeSchedulable` pre-filter (`#8919`).

## 3. The 4 `kubernetes_live` kinds → SDK `FactDeclaration`

`pod_template` at `1.3.0`, the other three at `1.0.0`
(`cloud/kubernetes_live.go:48-55`):

```json
[
  {"kind": "kubernetes_live.namespace", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "kubernetes_live.pod_template", "schema_versions": ["1.3.0"],
   "source_confidence": ["reported"]},
  {"kind": "kubernetes_live.relationship", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "kubernetes_live.warning", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]}
]
```

Plus the 8 posture kinds at `1.0.0` (`SecretsIAMSchemaVersionV1`),
`source_confidence: ["reported"]`: `k8s_service_account`,
`k8s_service_account_token_posture`, `k8s_rbac_role`, `k8s_rbac_binding`,
`k8s_workload_identity_use`, `k8s_gcp_workload_identity_binding`,
`eks_irsa_annotation`, `secrets_iam_coverage_warning`
(`eks_pod_identity_association` is never emitted by this collector).

SDK `Fact` mapping (`collector/types.go:125-135`): `stable_key` ← envelope
stable key, `source_confidence` ← `reported`,
`source_ref.scope_id` ← cluster scope, `source_ref.generation_id` ← committed
generation, `payload` ← factschema-encoded contract payload,
`redactions` ← fingerprint/annotation-allowlist redactions. Posture facts
carry `collector_kind = "secrets_iam_posture"`; the external producer must
preserve the dual-kind emission, not normalize it.

## 4. `ClusterTarget` JSON (operator shape, unchanged by extraction)

`loadRuntimeConfig` (`go/cmd/collector-kubernetes-live/config.go:25-52`) keys:
`cluster_id`, `display_name`, `provider`, `gcp_workload_pool`, `environment`,
`source_uri`, `fencing_token`, `auth_mode`, `kubeconfig_path`, `kube_context`,
`qps`, `burst`, plus `ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID` and
`ESHU_KUBERNETES_LIVE_POLL_INTERVAL` (`5m` default). Secrets travel by file
path or in-cluster mount, never inline. There is no empty-successor
attestation in this collector (no `Generation.trusted_deletion` analog).

## 5. Telemetry the external producer must preserve

| Signal | Name | Emitted at |
| --- | --- | --- |
| span | `kubernetes_live.snapshot` | `source.go:101` |
| span | `kubernetes_live.api_call` | `recordAPICall`, `source.go:187` |
| histogram | `eshu_dp_kubernetes_list_duration_seconds` (`instruments.go:5520`) | `source.go:132` |
| counter | `eshu_dp_kubernetes_api_calls_total` (`instruments.go:3629`) | `recordAPICall` |
| counter | `eshu_dp_kubernetes_resources_listed_total` (`instruments.go:3637`) | `recordResourcesListed` |
| counter | `eshu_dp_kubernetes_facts_emitted_total` (`instruments.go:3645`) | `append`, `builder.go:327` |
| counter | `eshu_dp_kubernetes_warnings_total` (`instruments.go:3653`) | `emitWarning` |

Labels stay `operation`, `result`, `resource_scope` (closed enum,
`ResourceScope*`), `fact_kind`, `reason` — never namespace, object, or image
names. The host additionally records grant decisions per emission (`#6709`).

## 6. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary). External
   fails closed on missing/expired/revoked grant at install, activation, and
   every emission. See [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree calls `postgres.NewIngestionStore` directly
   (`service.go:56`). External returns facts; core commits.
3. **Claim control**: there is no in-tree `ClaimedService` for this collector
   (poll only). External receives a bounded `Claim` and returns a `Result`;
   transitions stay `eshu-workflow`.
4. **Retry taxonomy**: in-tree has none (partial is evidence, errors retry at
   the next poll). The shim (§1) defines the `retryable` set.
5. **Annotation allowlist**: enforced adapter-side
   (`filterIdentityAnnotations`); the external producer must implement the
   equivalent capture boundary, or unbounded annotation values (including
   `last-applied-configuration` blobs that can embed Secret material) reach
   facts.
6. **Posture `source_uri`**: the shared posture seam carries it verbatim
   (EXTRACTION_PREP §10). The external producer must sanitize or drop it for
   posture facts until the secrets-IAM lane fixes the shared seam.
7. **Fencing token wire type**: in-tree `int64`; SDK `Claim.fencing_token`
   is a string. Format at the seam; the value must round-trip exactly.
