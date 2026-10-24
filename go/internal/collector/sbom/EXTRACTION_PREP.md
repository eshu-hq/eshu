# SBOM Attestation Collector — Extraction Prep Census (`#8865`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the cutover gate
(`#4047`) can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-sbom-attestation/` and
`go/internal/collector/sbom/` (including `document/` and `runtime/`).
Out of scope: `sdk/go` (Codex `#8851`), canonical SBOM projection
(core-owned), all other collectors, org repos, deployment.

## 1. Current packages

### Binary: `go/cmd/collector-sbom-attestation/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:43,95` | entry point, `-mode=claimed-live\|cassette` | `parseArgs`, `run` |
| `config.go:26` | operator target shape | `targetJSON` |
| `config.go:63` | env config | `loadClaimedRuntimeConfig` |
| `config.go:171` | target mapping | `mapTarget` |
| `service.go:36` | credential-free replay wiring | `buildCassetteService` |
| `service.go:60` | claimed wiring | `buildClaimedService` |
| `service.go:102` | document provider factory | `newDocumentProvider` |
| `service.go:116` | ECR auth wiring | `ecrAuthorizationClient` |
| `service.go:131` | claim IDs | `newClaimID` |

### Library: `go/internal/collector/sbom/document/` (moves with the collector)

| File | Role | Key symbol |
| --- | --- | --- |
| `types.go:10` | document collector family name | `CollectorKind = "sbom_document"` |
| `types.go:51` | warning taxonomy | `WarningReason*` |
| `envelope.go:21` | document envelope constructor | `newEnvelope` |
| `cyclonedx_fixture.go:22` | CycloneDX entry point | `CycloneDXFixtureEnvelopes` |
| `spdx_fixture.go:20` | SPDX entry point | `SPDXFixtureEnvelopes` |
| `cyclonedx_components.go`, `spdx_components.go` | per-component projection | component projection |
| `cyclonedx_types.go`, `spdx_types.go` | JSON shapes | format structs |
| `encode_helpers.go` | contract payload merge | `mergeContractPayloadNoError` |

### Library: `go/internal/collector/sbom/runtime/` (moves with the collector)

| File | Role | Key symbol |
| --- | --- | --- |
| `source.go:35` | claim-driven source | `ClaimedSource` |
| `source.go:45` | config validation | `NewClaimedSource` |
| `source.go:81` | per-claim collection | `NextClaimed` |
| `source.go:144,171` | SBOM parse dispatch (production `document/` caller) | `envelopes` |
| `config.go:51,59` | source/target shapes | `SourceConfig`, `TargetConfig` |
| `provider.go:54` | document fetch | `HTTPProvider.FetchDocument` |
| `referrer_client.go` | OCI referrer client seam | `ReferrerClientFactory`, `ECRReferrerClientFactory` |
| `attestation.go:42` | in-toto statement envelopes | `attestationEnvelopes` |
| `attestation_slsa.go:41` | SLSA provenance envelopes | `attestationSLSAProvenanceEnvelopes` |
| `encode_helpers.go` | contract payload merge | `mergeContractPayloadNoError` |

### Document/projection boundary (exact, per `#8865` body)

`document/` moves with the collector; its production caller is
`runtime/source.go:144,177-178` (`envelopes()` dispatches to
`document.CycloneDXFixtureEnvelopes` / `document.SPDXFixtureEnvelopes`).
Document-path envelopes stamp `CollectorKind` and `SourceSystem`
`"sbom_document"` (`document/types.go:10`, `document/envelope.go:30,36`);
attestation-path envelopes stamp `"sbom_attestation"`
(`runtime/attestation.go:236,242`). Canonical SBOM projection stays
core-owned: `go/internal/reducer/sbomattest/`,
`go/internal/projector/sbom/attestation/`, and the SLSA hooks in
`go/internal/reducer/containerimage/`.

## 2. Fact kinds and versions

Declared in `go/internal/facts/supply/chain/sbom_attestation.go`.
All eight kinds are at schema `1.1.0`
(`SBOMAttestationSchemaVersionV1`; bumped 1.0.0 → 1.1.0 in `#5456` for
additive-optional SLSA materials/config_source fields):

| Kind | Version | Emitter | Constructor |
| --- | --- | --- | --- |
| `sbom.document` | `1.1.0` | document parser | `cyclonedx_fixture.go:140`, `spdx_fixture.go:114` |
| `sbom.component` | `1.1.0` | document parser | `*_components.go` |
| `sbom.dependency_relationship` | `1.1.0` | document parser | `document/envelope.go:132` |
| `sbom.external_reference` | `1.1.0` | document parser | `document/envelope.go:159` |
| `sbom.warning` | `1.1.0` | document parser + runtime | `document/envelope.go:111`, `runtime/attestation.go:190` |
| `attestation.statement` | `1.1.0` | runtime | `runtime/attestation.go:113` |
| `attestation.slsa_provenance` | `1.1.0` | runtime | `runtime/attestation_slsa.go:214` |
| `attestation.signature_verification` | `1.1.0` | runtime | `runtime/attestation.go:155` |

Family declaration: `sbom_attestation.go:78-104`
(`SBOMAttestationFamilyDeclaration`, family `sbom_attestation`, truth profile
`provider_gated`). Producer record is already cutover-shaped
(`sbom_attestation.go:101`): `collector-sbom-attestation`, authority
`core_issued_grant`, planned repo
`eshu-hq/eshu-collector-sbom-attestation`. **Open discrepancy:** the record
binds the collector to 5 kinds (`attestation.*`,
`sbom.dependency_relationship`, `sbom.external_reference`) while the runtime
also emits `sbom.document`, `sbom.component`, and `sbom.warning` through the
`document/` parser — proven by parity scopes 1, 2, and 4. The same three
kinds are additionally bound to `scanner-worker` as `core_owned`. The grant
draft ([PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md)) covers all 8 emitted
kinds; the owner must reconcile the record before issuance.

## 3. Consumers (must agree at cutover)

- Attachment reducer: `go/internal/reducer/sbomattest/` (index, classify,
  writer; payload keys in `sbom_attestation_attachment_index.go` are the
  parser contract — `document/AGENTS.md` non-negotiable).
- Container-image reducer SLSA hooks: `go/internal/reducer/containerimage/`
  (`container_image_identity_slsa.go`, `container_image_identity.go`).
- Projection: `go/internal/projector/sbom/attestation/`.
- Query/read: `GET /api/v0/supply-chain/sbom-attestations/attachments{,/count,/inventory}`
  (`go/internal/query/auth_scoped_routes_completeness.go:212-214`).
- MCP: supply-chain tools (`go/internal/mcp/tools_supply_chain.go` and route
  registry entries).
- Golden fixtures: `testdata/cassettes/sbomattestation/supply-chain-demo.json`,
  `testdata/cassettes/replayoffline/sbom-attestation-attachment.cost-budget.json`.
- Co-emitter of three kinds: `go/internal/collector/scannerworker/sbomgenerator/`
  (bounded SBOM **generation**; this runtime only fetches and parses
  already-published documents — `runtime/README.md` boundary).

## 4. Durable-state owner

The collector owns **no** durable state. Durable writes stay core-owned:

- Fact commit: `postgres.NewIngestionStore` (`service.go:47,76`).
- Claim control: `postgres.NewWorkflowControlStore` (`service.go:80`).
- Claim lifecycle: `collector.ClaimedService` (heartbeat strictly below lease
  TTL per `cmd` AGENTS.md invariants).
- In-tree transient state only: the `targets` map and provider handles in
  `ClaimedSource` (`runtime/source.go:37-42`); generation IDs and fencing
  tokens arrive on the work item and are reused on retry, so redelivery is
  idempotent.

Admission, persistence, reducer queue, projection, and retraction remain Eshu
core per the extraction policy. The external producer must submit a bounded
result; it never receives Postgres, graph, queue, lease, reducer, API, MCP, or
workflow handles.

## 5. Protocols

- `configured_source`: one bounded HTTPS GET per claim (`provider.go:54`),
  `max_bytes` cap (default 10 MiB, `runtime/config.go:16`), SDK default HTTP
  client, status/transport failures wrapped in bounded `HTTPError` causes
  under the `RegistryFailure` class/details contract.
- `oci_referrer`: OCI Distribution manifest/blob fetch through
  `ReferrerClient` (`provider.go:27-30`); `provider=ecr` mints short-lived
  basic-auth from the AWS `GetAuthorizationToken` exchange
  (`service.go:97-129`, `referrer_client.go`); other providers stay on the
  static-credential path.
- Credentials by env reference only (`username_env`, `password_env`,
  `bearer_token_env`); source URIs stored in facts are redacted (no userinfo,
  query, or fragment — `runtime/source.go:200-221`).
- Document parsing: CycloneDX/SPDX JSON via `document/`; in-toto statements
  via `attestation.go:87` (`UseNumber` decode); SLSA v1/v0.2 predicates via
  `attestation_slsa.go`. Parser facts keep `verification_status` blank and
  `SourceConfidence = reported`; only the attachment reducer may promote to
  `attached_verified`.
- Public target contract: `collector-sdk/v1alpha1`
  (`sdk/go/collector/types.go:10`).
- Cassette replay: `File` load and validate (`LoadFile`, `ParseAndValidate`
  in `go/internal/replay/cassette/format.go`); binary flag
  `-mode=cassette -cassette-file=<path>` (`main.go:43-64`).

Numeric-fidelity note (`#8851` / `#8877` lineage):
`sbom.document` payloads carry number-valued `component_count`,
`dependency_count`, and `warning_count` (`document/cyclonedx_fixture.go:104-106`).
`DecodeWorkItemProjectMetadata` governs `work_item.project_metadata` facts,
which this lane neither emits nor consumes, so that exact issue is **not** a
blocker here. The analogous hazard — a JSON round-trip (cassette file,
fixture, SDK boundary) decoding counts as `float64`/`json.Number` — is
handled in the parity pack by comparing canonical JSON bytes rather than Go
type identity. The cutover implementation must preserve the same rule, and the
ingestion/commit path must treat whole-number `5` and `5.0` as the same
count.

## 6. Artifacts

- Today: single in-tree binary `collector-sbom-attestation`
  (`main.go:25`). No published artifact.
- At cutover: digest-pinned OCI artifact executed via
  `OCIRunner` in `go/internal/collector/extensionhost/oci.go`, same SDK
  contract as `ProcessRunner`. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` (diagnostic reads `extraction_candidate`). Not `ready_for_cutover`:
repository, release, operational, and rollback proof are absent, and the
public SDK surface is owned by `#8851`. Roadmap owners: `#6707` inventory,
`#6708` SDK/fact-schema, `#6709` producer delegation, `#6710` template and
compatibility harness, `#4047` per-collector cutover gate.

## 8. `#8865` acceptance sweep at `origin/main`

| Acceptance item | State at PREP |
| --- | --- |
| Named owners, production target, configuration source | **Open** — owner-confirmed on the issue; QA EKS access unavailable from this machine, `NOT_CHECKED` |
| Census, kinds, consumers, state, protocols, artifacts, stage | **Met in PREP** by this document |
| Source parity on representative + hostile inputs | **Met in-tree side** by `runtime/source_parity_8865_test.go` + `runtime/testdata/8865-parity-sbom-attestation.json` (AC3) |
| Extracted-path admitted facts, projection, graph, API, MCP truth | **Open** — needs `#4047` gate + deployment |
| Retry, duplicates, ordering, cancellation, revoked authority, stale claims, partial/ambiguous failures | **Open** — duplicates + malformed + grant-free delta pinned in-tree; revocation/staleness need the extracted path |
| Bounded resources, no regression, health/readiness/freshness/telemetry | **Open** — in-tree no-regression evidence recorded in `runtime/README.md` and `document/README.md`; extracted-path proof outstanding |
| Credentials, provenance, vuln response, oldest/newest pairs | **Partial** — credential handling and provenance recorded here; version pairs need `#6710` + `#8851` |
| Independent build/release/deploy on public contracts | **Open** — SDK pins verified (see [SHIM_MAP](SHIM_MAP.md)), template outstanding |
| Rollback rehearsal | **Draft recorded** ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)); rehearsal **open** |
| Review, local proof, stable CI, removal after acceptance | **Open** — local proof in this lane's commit; removal explicitly **not** in PREP |

## 9. Prerequisites for cutover

1. Published SDK + fact-schema modules (`#6708`, `#8851` Codex lane) at the
   pinned `v0.1.0-rc.1` coordinates (verified present and sufficient — see
   [SHIM_MAP](SHIM_MAP.md); no missing symbols).
2. Producer-record reconciliation for the 5-vs-8 kind discrepancy (§2) and
   first-party producer delegation live (`#6709`); grant draft recorded in
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
3. Collector repository from the `#6710` template with the compatibility
   harness matrix (newest collector × oldest core and reverse).
4. Parity proof on representative + hostile inputs via the fixture pack in
   `runtime/testdata/8865-parity-sbom-attestation.json` and
   `runtime/source_parity_8865_test.go` (AC3, in-tree side).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)).
