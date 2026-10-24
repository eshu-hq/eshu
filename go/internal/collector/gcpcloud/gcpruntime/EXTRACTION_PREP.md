# GCP Cloud Collector — Extraction Prep Census (`#8856`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the cutover gate
(`#4047`) can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-gcp-cloud/` and
`go/internal/collector/gcpcloud/` (including `gcpruntime/`, `catalog/`,
`extractor/`, `freshness/`). Out of scope:
`sdk/go` (Codex `#8851`), all other collectors, org repos, deployment.

## 1. Current packages

### Binary: `go/cmd/collector-gcp-cloud/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:46,186` | entry point, `-mode=fixture\|claimed-live\|cassette` | `parseArgs`, `run`, `buildRuntimeRunner` |
| `config.go:57,72` | declarative file config + fixture-file mapping | `loadFileConfig`, `runtimeConfig` |
| `claimed_config.go:66` | claim-driven env + instance config | `loadClaimedRuntimeConfig` |
| `service.go:48` | fixture wiring (offline `FixturePageProvider`) | `buildCollectorService`, `buildSource` |
| `service.go:76` | claimed-live wiring (`LiveClient` via ADC) | `buildClaimedService` |
| `service.go:179` | credential-free replay wiring | `buildCassetteService` |
| `status_committer.go:27` | bounded claim-metric wrap on the committer | `newGCPStatusCommitter` |

### Library: `go/internal/collector/gcpcloud/`

| File | Role | Key symbol |
| --- | --- | --- |
| `catalog/gcp_shared.go:217` | collector family name | `CollectorKind = "gcp"` |
| `catalog/gcp_runtime.go:69` | poll cadence default | `DefaultPollInterval = 30m` |
| `types.go:9` | pre-normalization observation | `ResourceObservation` (+ IAM/relationship/DNS/image/tag sub-observations) |
| `normalize.go` | CAI name + ancestor normalization | `NormalizeAncestry`, `ProjectIDFromFullName` |
| `redaction.go` | keyed fingerprinting choke point | `FingerprintMember`, `FingerprintLabelValues`, `RedactionPolicyVersion = "gcp-cloud-2026-06-09"` |
| `parse.go` | safe CAI page parsing (drops raw data blob) | `ParseAssetsListPage`, `ParseSearchAllResourcesPage` |
| `envelope.go:22,115` | durable fact envelopes | `NewCloudResourceEnvelope`, `NewCollectionWarningEnvelope` |
| `relationship.go:28`, `tag_observation.go:55`, `iam_policy_observation.go:62`, `dns_record.go:55`, `image_reference.go:54` | family envelopes | `NewCloudRelationshipEnvelope`, `NewTagObservationEnvelope`, `NewIAMPolicyObservationEnvelope`, `NewDNSRecordEnvelope`, `NewImageReferenceEnvelope` |
| `gcp_secrets_iam.go:39` | secrets/IAM mirror projection | `secretsIAMEnvelopes` (`gcp_iam_principal`, `gcp_iam_trust_policy`, `gcp_iam_permission_policy`) |
| `generation.go:32,424` | accumulation, dedupe, fencing | `Generation`, `GenerationTracker`, `ErrStaleGeneration` |
| `metrics.go:39` | scoped OTEL instruments | `NewMetrics` (`eshu_dp_gcp_cloud_*`) |
| `extractor/registry` + `extractor/<group>/` | per-asset-type typed-depth extractors | `RegisterAssetExtractor` (see `AGENTS-extractors-1.md`, `AGENTS-extractors-2.md`) |
| `freshness/` | provider-to-Eshu lag evidence | freshness `pubsub.go`, `types.go` |
| `gcpruntime/source.go:33` | unclaimed poll source | `Source.Next` |
| `gcpruntime/claimed_source.go:20` | claim-scoped source | `Source.NextClaimed`, `scopeForClaim` |
| `gcpruntime/config.go:19,34` | declarative runtime config | `Config`, `ScopeConfig`, `DeriveScopeID` |
| `gcpruntime/pageprovider.go:31,48` | transport seam | `PageProvider`, `FixturePageProvider` |
| `gcpruntime/tagprovider.go` | direct/effective tag API seam | `TagProvider` (opt-in per scope) |
| `gcpruntime/liveclient.go:22` | REST transport (never a default) | `LiveClient`, `ProviderWarning` |
| `gcpruntime/telemetry.go:20` | bounded-label record helpers | `recordEmission`, `recordWarning`, `recordClaim` |

## 2. Fact kinds and versions

Declared in `go/internal/facts/cloud/gcp.go`
(`GCPCloudResourceFactKind` through `GCPImageReferenceFactKind`) and
`go/internal/facts/identity/iam.go` (mirror kinds):

| Kind | Version | Constructor |
| --- | --- | --- |
| `gcp_cloud_resource` | `1.1.0` (typed-depth `attributes`/`correlation_anchors`) | `envelope.go:22` |
| `gcp_cloud_relationship` | `1.0.0` | `relationship.go:28` |
| `gcp_tag_observation` | `1.0.0` | `tag_observation.go:55` |
| `gcp_iam_policy_observation` | `1.0.0` | `iam_policy_observation.go:62` |
| `gcp_dns_record` | `1.0.0` | `dns_record.go:55` |
| `gcp_image_reference` | `1.0.0` | `image_reference.go:54` |
| `gcp_collection_warning` | `1.0.0` | `envelope.go:115` |
| `gcp_iam_principal` | `1.0.0` (mirror, service-account grantees) | `gcp_secrets_iam.go:84` via `posture` |
| `gcp_iam_permission_policy` | `1.0.0` (mirror) | `gcp_secrets_iam.go:94` via `posture` |
| `gcp_iam_trust_policy` | `1.0.0` (mirror, impersonation bindings) | `gcp_secrets_iam.go:70` via `posture` |

Family declaration: `gcp.go:98-122` (`GCPFamilyDeclaration`).
Producer record is already cutover-shaped (`gcp.go:120`):
`collector-gcp-cloud`, authority `core_issued_grant`, 7 kinds,
planned repo `eshu-hq/eshu-collector-gcp-cloud`, in-tree path
`go/internal/collector/gcpcloud` (correct — unlike the OCI lane, no stale
path here).

**Open authority item (not fixed by this PREP):** the 3 mirror kinds are
emitted by this collector's `Generation`, but their producer record names
`secrets-iam-posture` (`go/internal/facts/identity/iam.go`, producer `secrets-iam-posture`), not
`collector-gcp-cloud`. The mirror envelopes also stamp
`collector_kind = "secrets_iam_posture"` (`posture/types.go:10`), not
`"gcp"` — pinned by `source_parity_8856_test.go`. The cutover grant must
reconcile both (re-stamp vs dual-family manifest) before production
cutover; see [SHIM_MAP](SHIM_MAP.md) and
[PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).

## 3. Consumers (must agree at cutover)

- Canonical projection: `go/internal/projector/cloud/gcp/resource_materialization_intents.go`,
  shared inventory `go/internal/projector/cloud/inventory/reducer_intent.go`,
  multi-cloud drift `go/internal/projector/cloud/runtime/drift/multi/`.
- Reducer domains: `gcp_resource_materialization` (family truth profile
  `provider_gated`); `gcp_relationship_materialization`
  (`go/internal/reducer/gcp_relationship_materialization.go`);
  container-image identity (`go/internal/reducer/containerimage/`, incl.
  `container_image_identity_gcp_test.go`); `secrets_iam_trust_chain` for the
  3 mirror kinds.
- Reducer decode: `sdk/go/factschema/decode_gcp.go`,
  `decode_secretsiam_source.go` (GCP `Encode*` present in RC1, import-path
  only delta vs tree).
- Query/read: `GET /api/v0/cloud/inventory`, `GET /api/v0/cloud/resources`
  (`go/internal/query/cloud_inventory_read_model.go`,
  `cloud_inventory_readback.go`); secrets/IAM posture
  `GET /api/v0/secrets-iam/posture-summary` for mirror kinds.
- Unconsumed-by-design (Round-2 verified, recorded in the family
  declaration): `gcp_collection_warning`, `gcp_dns_record`,
  `gcp_iam_policy_observation`; `gcp_tag_observation` is read-only.

## 4. Durable-state owner

The collector owns **no** durable state. Durable writes stay core-owned:

- Fact commit: `postgres.NewIngestionStore` (`service.go:62,98`).
- Claim control: `postgres.NewWorkflowControlStore` (`service.go:102`).
- Claim lifecycle: `collector.ClaimedService` (the `ClaimedService` runner
  in `go/internal/collector/claimed_service.go`).
- In-tree transient state only: `Source.scopeIndex`/`drained` cursor
  (`gcpruntime/source.go:55-56`); `Generation` maps and
  `GenerationTracker` tokens reset per process and never persist.

Admission, persistence, reducer queue, projection, and retraction remain Eshu
core per the extraction policy. The external producer must submit a bounded
result; it never receives Postgres, graph, queue, lease, reducer, API, MCP, or
workflow handles.

## 5. Protocols

- Cloud Asset Inventory: `assets.list` + `searchAllResources` page drain
  with continuation-token resume; a token the provider cannot resume becomes
  a `page_token_expired` warning (`gcpruntime/source.go:142-187`), never
  silent truncation.
- Typed provider warnings (`ProviderWarning`, `liveclient.go:30`) become
  `gcp_collection_warning` facts: quota, unavailable, unsupported,
  permission-hidden, stale, redaction.
- Direct/effective Resource Manager tag APIs: per-scope opt-in via
  `DirectTagsEnabled` / `EffectiveTagsEnabled` (`config.go:64-67`); tag
  values fingerprinted before emission.
- Live transport: `LiveClient` REST via ambient ADC (`claimed-live` mode
  only, `service.go:76-123`); fixture mode always uses
  `FixturePageProvider`; cassette mode replays without credentials.
- Auth: credential by NAME only (`ScopeConfig.CredentialRef`); material
  never in config, logs, labels, or facts. Redaction key from a file path,
  never logged.
- Public target contract: `collector-sdk/v1alpha1`
  (`sdk/go/collector/types.go`, `ProtocolVersionV1Alpha1`).
- Scope wire shape: `gcp:<parent_kind>:<parent_id>:<asset_family>:<content_family>:<location_bucket>`
  (`DeriveScopeID`, `config.go:149`).

## 6. Artifacts

- Today: single in-tree binary `collector-gcp-cloud` (helm
  `deployment-gcp-cloud-collector.yaml`, helper
  `_gcp_cloud_collector_helpers.tpl`). No published artifact.
- At cutover: digest-pinned OCI artifact executed via
  `OCIRunner` in `go/internal/collector/extensionhost/oci.go`, same SDK
  contract as `ProcessRunner`. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` (diagnostic reads `extraction_candidate`). Not `ready_for_cutover`:
repository, release, deployment, and rollback proof are absent, and the public
SDK surface is owned by `#8851`. Roadmap owners: `#6707` inventory,
`#6708` SDK/fact-schema, `#6709` producer delegation, `#6710` template and
compatibility harness, `#4047` per-collector cutover gate.

## 8. Prerequisites for cutover

1. Published SDK + fact-schema modules (`#6708`, `#8851` Codex lane):
   `collector`/`factschema` `v0.1.0-rc.1` verified present on the proxy with
   the full GCP surface (see [SHIM_MAP](SHIM_MAP.md)).
2. First-party producer delegation live (`#6709`); grant draft recorded in
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md), including the mirror-kind
   authority reconciliation (§2 open item).
3. Collector repository from the `#6710` template with the compatibility
   harness matrix (newest collector × oldest core and reverse).
4. Parity proof on representative + hostile inputs via the fixture pack in
   `gcpruntime/testdata/8856-parity-gcp-cloud.json` and
   `gcpruntime/source_parity_8856_test.go` (AC3, in-tree side).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)).
