# OCI Registry Collector — Extraction Prep Census (`#8861`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the cutover gate
(`#4047`) can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-oci-registry/` and
`go/internal/collector/registry/oci/` (including `runtime/`). Out of scope:
`sdk/go` (Codex `#8851`), all other collectors, org repos, deployment.

## 1. Current packages

### Binary: `go/cmd/collector-oci-registry/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:71,94` | entry point, `-mode=live\|cassette` | `parseArgs`, `run` |
| `config.go:36` | env config, `targetJSON` | `loadRuntimeConfig`, `loadClaimedRuntimeConfig` |
| `service.go:67` | unclaimed wiring | `buildCollectorService` |
| `service.go:99` | claimed wiring | `buildClaimedService` |
| `service.go:43` | credential-free replay wiring | `buildCassetteService` |
| `service.go` | provider factory | `providerFactory` |

### Library: `go/internal/collector/registry/oci/`

| File | Role | Key symbol |
| --- | --- | --- |
| `types.go:13` | collector family name | `CollectorKind = "oci_registry"` |
| `types.go:20` | provider set (7 adapters) | `ProviderDockerHub…ProviderAzureContainerRegistry` |
| `envelope.go:25,69,133,168,199,228` | 6 envelope constructors | `NewRepositoryEnvelope` … `NewReferrerEnvelope` |
| `warning.go:18` | warning envelope | `NewWarningEnvelope` |
| `identity.go:17,43` | stable identity | `NormalizeRepositoryIdentity`, `NormalizeDescriptorIdentity` |
| `encode_helpers.go` | contract payload merge | `mergeContractPayload` |
| `distribution/client.go` | OCI Distribution client | tag/manifest/blob/referrers calls |
| `distribution/token.go` | token auth | bearer negotiation |
| `distribution/tag_pagination.go` | bounded tag windows | tag pagination |
| `distribution/redirect.go` | registry redirects | redirect policy |
| `runtime/source.go:65` | unclaimed poll source | `Source.Next` |
| `runtime/claimed_source.go:25` | claim-scoped source | `ClaimedSource.NextClaimed` |
| `runtime/config.go:21,38` | scan config | `Config`, `TargetConfig` |
| `runtime/transport.go` | retry classification | `isSkippableTransientError` |
| `runtime/freshness.go` | generation hints | `ociScanFreshnessHint` |
| `runtime/manifest.go` | manifest parse | `parseManifest`, `manifestDigest` |
| `runtime/source_references.go` | reference listing | `listReferences` |
| `runtime/config_provenance.go` | config-blob labels | `configProvenanceLabels` |
| `runtime/tls_config.go` | TLS modes | `TLSConfig` |
| `acr/`, `dockerhub/`, `ecr/`, `gar/`, `ghcr/`, `harbor/`, `jfrog/` | provider adapters | one `adapter.go` each |

## 2. Fact kinds and versions

Declared in `go/internal/facts/supply/chain/oci_registry.go:12-43`. All seven
kinds are at schema `1.0.0`:

| Kind | Version | Constructor |
| --- | --- | --- |
| `oci_registry.repository` | `1.0.0` | `envelope.go:25` |
| `oci_registry.image_tag_observation` | `1.0.0` | `envelope.go:69` |
| `oci_registry.image_manifest` | `1.0.0` | `envelope.go:133` |
| `oci_registry.image_index` | `1.0.0` | `envelope.go:168` |
| `oci_registry.image_descriptor` | `1.0.0` | `envelope.go:199` |
| `oci_registry.image_referrer` | `1.0.0` | `envelope.go:228` |
| `oci_registry.warning` | `1.0.0` | `warning.go:18` |

Family declaration: `oci_registry.go:79-107` (`OCIRegistryFamilyDeclaration`).
Producer record is already cutover-shaped (`oci_registry.go:105`):
`collector-oci-registry`, authority `core_issued_grant`, all 7 kinds,
planned repo `eshu-hq/eshu-collector-oci-registry`. Note: the record's
`Path` still reads `go/internal/collector/ociregistry`, which does not
exist; the actual in-tree path is `go/internal/collector/registry/oci/`.

## 3. Consumers (must agree at cutover)

- Canonical projection: `go/internal/projector/canonical/oci_registry.go`
  (warning kind intentionally not consumed there).
- Projector decode: `go/internal/projector/decode/oci_registry.go`.
- Reducer domain: `container_image_identity`
  (`oci_registry.go:89` truth profile `provider_gated`).
- Query/read: `go/internal/query/container_image_identities_test.go:198`,
  `container_image_candidate_explanation.go:81-105`, `images_test.go:53`.
- Golden fixture: `testdata/cassettes/ociregistry/supply-chain-demo.json`,
  projected by `go/internal/projector/canonical/oci_registry_cassette_test.go`.

## 4. Durable-state owner

The collector owns **no** durable state. Durable writes stay core-owned:

- Fact commit: `postgres.NewIngestionStore` (`service.go:54,80,110`).
- Claim control: `postgres.NewWorkflowControlStore` (`service.go:114`).
- Claim lifecycle: `collector.ClaimedService`
  (`go/internal/collector/claimed_service.go:59`).
- In-tree transient state only: `Source.next` cursor and
  `transportFailures` counters (`runtime/source.go:57-61`); both reset per
  process and never persist.

Admission, persistence, reducer queue, projection, and retraction remain Eshu
core per the extraction policy. The external producer must submit a bounded
result; it never receives Postgres, graph, queue, lease, reducer, API, MCP, or
workflow handles.

## 5. Protocols

- OCI Distribution Spec: tag list, manifest GET (digest header or computed),
  blob GET (bounded provenance read), Referrers API with unsupported-API
  fallback to an in-band `warning` envelope (`runtime/source.go:369-380`).
- Auth: anonymous or credentialed per target (`types.go:47-58`); bearer token
  negotiation in `distribution/token.go`.
- Target wire shape: `ESHU_OCI_REGISTRY_TARGETS_JSON` decoded by
  `targetJSON` (`config.go:36-69`); runtime shape `TargetConfig`
  (`runtime/config.go:38-67`).
- Public target contract: `collector-sdk/v1alpha1`
  (`sdk/go/collector/types.go:10`).
- Cassette replay: `go/internal/replay/cassette/format.go:28-113`;
  binary flag `-mode=cassette -cassette-file=<path>` (`main.go`).

## 6. Artifacts

- Today: single in-tree binary `collector-oci-registry`
  (`serviceName` in `main.go`). No published artifact.
- At cutover: digest-pinned OCI artifact executed via
  `go/internal/collector/extensionhost/oci.go:42-71` (`OCIRunner`), same SDK
  contract as `ProcessRunner`. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` (diagnostic reads `extraction_candidate`). Not `ready_for_cutover`:
repository, release, operational, and rollback proof are absent, and the public
SDK surface is owned by `#8851`. Roadmap owners: `#6707` inventory,
`#6708` SDK/fact-schema, `#6709` producer delegation, `#6710` template and
compatibility harness, `#4047` per-collector cutover gate.

## 8. Prerequisites for cutover

1. Published SDK + fact-schema modules (`#6708`, `#8851` Codex lane).
2. First-party producer delegation live (`#6709`); grant draft recorded in
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
3. Collector repository from the `#6710` template with the compatibility
   harness matrix (newest collector × oldest core and reverse).
4. Parity proof on representative inputs via the fixture pack in
   `runtime/testdata/8861-parity-oci-registry.json` and
   `runtime/source_parity_8861_test.go` (AC3, in-tree side).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)).
