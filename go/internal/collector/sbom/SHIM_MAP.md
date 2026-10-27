# Internal → Public Shim Map (`#8865`, analysis artifact)

This is a **design map, not an implementation**. It records how each in-tree
seam becomes a public SDK exchange at extraction, so the `#6710` template
repository and the `#4047` parity gate share one interpretation. Nothing here
changes runtime behavior.

Pinned SDK (exact; a reviewed change that bumps `go.mod` or adds an
eshu-sdk dependency MUST update these pins in the same change — enforced
by `runtime/shim_pin_8865_test.go:TestSBOM8865ShimPinsMatchGoMod`, which
fails on version drift in either direction and on built SDK modules with
no recorded pin. Newer tags MUST NOT be substituted without that reviewed
update):

- `github.com/eshu-hq/eshu-sdk/collector v0.1.0-rc.3`
- `github.com/eshu-hq/eshu-sdk/factschema v0.1.0-rc.7`

Pin history: proxy-verified 2026-10-24 at collector/factschema
`v0.1.0-rc.1`; in-tree moved to standalone modules at collector `rc.3` +
factschema `rc.4` via `686ad9c269`, to factschema `rc.5` via `d103071673`,
to factschema `rc.6` via `df21acbf30` (#9252, records RC6/RC7 proxy
availability), and to factschema `rc.7` with main (proxy-verified
2026-10-27 via `go mod download`). The extracted port
(`eshu-collector-sbom-attestation` main, observed 2026-10-27) still builds
at collector/factschema `v0.1.0-rc.2` — that drift is open until the port
re-pins and re-proves the #4047 parity gate.

## 1. Claim path: `ClaimedService` / `NextClaimed` → SDK `Claim` / `Result`

This collector is claim-only: there is no unclaimed poll loop. In-tree,
`ClaimedSource.NextClaimed` (`runtime/source.go:81`) binds a
`workflow.WorkItem` to one configured target by `scope_id`
(`source.go:101-108`), stamps `item.CurrentFencingToken`, and reuses
`item.GenerationID` so retries are idempotent. The cassette binary
(`service.go:36-58`) replays recorded generations through the shared commit
boundary for the golden-corpus gate only; it is not a production path.

| In-tree outcome | SDK `ResultState` (`types.go:31-41`) | Host action |
| --- | --- | --- |
| envelopes committed (parsed, SLSA, verification, warnings) | `complete` | validate facts, complete claim |
| malformed document → `sbom.document` (malformed) + `sbom.warning` | `complete` with warning facts (or `partial` + `Status{warning}` if the host treats parser warnings as incompleteness — cutover decision, default `complete` matches in-tree) | commit reachable evidence |
| fetch failure, retryable (transport / retryable HTTP status, `HTTPError` cause) | `retryable` | release claim, bounded retry |
| terminal (auth, terminal HTTP, contract violation, unknown target/scope, kind mismatch) | `terminal` | terminal-fail claim |
| cancellation | (no result) | stop, no commit |

Duplicate delivery stays safe: stable keys are deterministic
(`document/envelope.go:46-53,80-86,101-109`, `runtime/attestation.go:252-272`),
generation IDs are reused on retry, and generation-scoped commit is idempotent
at the core boundary. Pinned by
`TestSBOM8865ParityDuplicateDeliveryIsStable`.

| In-tree field | SDK `Claim` (`types.go:88-101`) |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorSBOMAttestation`) | `collector_kind` |
| `item.CollectorInstanceID` vs `Config.CollectorInstanceID` | `instance_id` (mismatch rejects, `source.go:85-91`) |
| `item.ScopeID` | `scope.id` |
| `item.GenerationID` | `generation.id` (reused on retry) |
| `item.CurrentFencingToken` | `fencing_token` |
| `item.Attempt`, deadline, `WorkItemID` | `attempt`, `deadline`, `work_item_id` |
| per-target fetch config | `config_handle` (opaque; resolved host-side) |

Stale-claim rule (core-owned): the workflow control store, not the producer,
decides lease expiry and supersession. The producer must treat a claim it did
not `.Claim()` as foreign and never write for it.

Failure detail seam: the provider wraps configured-source status and
transport failures in bounded `sdk.HTTPError` causes under the
`RegistryFailure` class/details contract (`go/internal/collector/sdk/failure.go`,
`http.go`). That surface is `#8851`-owned; its RC1 counterpart is the
`transport/` subpackage (see §6). The external `Result` must carry the same
class/details so operators keep the current failure taxonomy.

## 2. The 8 `sbom.*` / `attestation.*` kinds at `1.1.0` → SDK `FactDeclaration`

One declaration per kind, each `schema_versions: ["1.1.0"]`
(`chain/sbom_attestation.go:50-59`). All carry
`source_confidence: ["reported"]`; parser statements additionally keep
`verification_status` blank (only the reducer promotes).

```json
[
  {"kind": "sbom.document", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]},
  {"kind": "sbom.component", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]},
  {"kind": "sbom.dependency_relationship", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]},
  {"kind": "sbom.external_reference", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]},
  {"kind": "attestation.statement", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]},
  {"kind": "attestation.slsa_provenance", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]},
  {"kind": "attestation.signature_verification", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]},
  {"kind": "sbom.warning", "schema_versions": ["1.1.0"],
   "source_confidence": ["reported"]}
]
```

SDK `Fact` mapping (`types.go:125-135`): `stable_key` ← envelope stable key,
`source_confidence` ← `reported`, `source_ref.scope_id` ← target `scope_id`,
`source_ref.generation_id` ← work-item generation, `payload` ← contract
payload merged by `mergeContractPayloadNoError` (`encode_helpers.go` in both
packages), `redactions` ← redacted source locators (userinfo/query/fragment
stripped, `runtime/source.go:200-221`).

Dual collector-kind boundary the host must accept: document-path facts carry
`collector_kind`/`source_system` `"sbom_document"`; attestation-path facts
carry `"sbom_attestation"`. Both are first-party emissions of this one
producer (see [EXTRACTION_PREP](EXTRACTION_PREP.md) §1); the grant check must
not reject either.

## 3. `targetJSON` operator shape (unchanged by extraction)

`targetJSON` (`go/cmd/collector-sbom-attestation/config.go:26-48`) keys:
`scope_id`, `source_type`, `artifact_kind`, `document_format`, `provider`,
`registry`, `registry_host`, `region`, `aws_profile`, `repository`,
`document_url`, `source_uri`, `source_record_id`, `subject_digest`,
`referrer_digest`, `verification_result`, `verification_policy`,
`username_env`, `password_env`, `bearer_token_env`, `max_bytes`. Secrets
travel by env reference, never inline. `max_bytes` defaults to 10 MiB
(`runtime/config.go:16,137-139`); negative values reject at validation.

## 4. Telemetry the external producer must preserve

The runtime emits no metrics, spans, or logs of its own from the fetch path
(no-observability-change evidence in `runtime/README.md`); operators diagnose
failed claims through workflow failure class/details and the fact/warning
surfaces. Preserved signals:

| Signal | Name | Emitted at |
| --- | --- | --- |
| log (info) | ECR `GetAuthorizationToken` selection (`provider`, `scope_id`, `repository`, `registry_host`; never the token) | `provider=ecr` referrer targets |
| counter | `eshu_dp_* SBOMAttestationAttachments{...}` (`attached_verified`, `attached_parse_only`, `unknown_subject`, `ambiguous_subject`, `unparseable`, `subject_mismatch`) | attachment reducer (core-owned, unchanged) |
| facts | `sbom.warning` per parser warning reason | document parser / runtime |

The host additionally records grant decisions per emission (`#6709`).

## 5. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary). External
   fails closed on missing/expired/revoked grant at install, activation, and
   every emission. See [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree calls `postgres.NewIngestionStore` directly
   (`service.go:47,76`). External returns facts; core commits.
3. **Claim control**: in-tree `ClaimedService` owns lease/heartbeat
   (`service.go:79-94`). External receives a bounded `Claim` and returns
   a `Result`; transitions stay `eshu-workflow`.
4. **Collector-kind duality**: in-tree commits both `sbom_document` and
   `sbom_attestation` facts through one service. External keeps that shape;
   the host admission check must key on the producer grant, not on a single
   collector-kind string.

## 6. RC1 verification (no missing symbols, one `#8851` confirmation)

Checked 2026-10-24 against the proxy-fetched modules (byte comparison with
the in-tree sources listed):

- `collector/types.go` ≡ in-tree `sdk/go/collector/types.go`: `Result`,
  `Claim`, `Scope`, `Generation`, `Fact`, `SourceRef`, `Status`,
  `FactDeclaration`, all five `ResultState` values, and
  `ProtocolVersionV1Alpha1 = "collector-sdk/v1alpha1"` all present.
- `factschema/sbom/v1/{document,attestation}.go` ≡ in-tree: every
  `Encode*` the lane calls (`EncodeSBOMDocument`, `EncodeSBOMComponent`,
  `EncodeSBOMDependencyRelationship`, `EncodeSBOMExternalReference`,
  `EncodeSBOMWarning`, `EncodeAttestationStatement`,
  `EncodeAttestationSLSAProvenance`,
  `EncodeAttestationSignatureVerification`) present.
- `collector/transport/*` ≡ in-tree `go/internal/collector/sdk/*`
  (`failure.go`, `http.go`, `transport.go`, `tls.go`) modulo the
  `sdk` → `transport` package rename and one relocated comment
  (`failure.go` diff is 5 lines). Behavioral equivalence is `#8851`'s call
  to confirm at relocation; this lane makes no claim beyond surface parity
  and edits nothing under `sdk/`.

**No SDK symbol needed by this lane is missing from `v0.1.0-rc.1`.**
No handoff to `#8851` for gaps; the standing handoff remains the
relocation/equivalence proof itself.
