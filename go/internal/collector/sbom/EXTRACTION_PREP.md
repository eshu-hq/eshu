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
`provider_gated`). Producer record is cutover-shaped
(`sbom_attestation.go:101`): `collector-sbom-attestation`, authority
`core_issued_grant`, planned repo
`eshu-hq/eshu-collector-sbom-attestation`, bound to all 8 emitted kinds.
**Reconciled:** the record previously bound the collector to 5 kinds
(`attestation.*`, `sbom.dependency_relationship`, `sbom.external_reference`)
while the runtime also emits `sbom.document`, `sbom.component`, and
`sbom.warning` through the `document/` parser — proven by parity scopes 1, 2,
and 4. The producer-record lane (`#8865`) widened the record to all 8; the
same three kinds remain additionally bound to `scanner-worker` as
`core_owned` (dual emission until the scanner-worker cutover). The grant
record ([PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md)) covers all 8 emitted
kinds and is now issued against the reconciled record.

## 3. Consumers (must agree at cutover)

- Attachment reducer: `go/internal/reducer/sbomattest/` (index, classify,
  writer; payload keys in `sbom_attestation_attachment_index.go` are the
  parser contract — `document/AGENTS.md` non-negotiable).
- Container-image reducer SLSA hooks: `go/internal/reducer/containerimage/`
  (`container_image_identity_slsa.go`, `container_image_identity.go`).
- Projection: `go/internal/projector/sbom/attestation/`.
- Query/read: `GET /api/v0/supply-chain/sbom-attestations/attachments{,/count,/inventory}`
  (`go/internal/query/auth_scoped_routes_completeness.go`, `scopedTokenAdvertisedRoutes`).
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
  query, or fragment — `runtime/source.go:200-221`). Source record IDs are
  carried verbatim today (`source.go:141` raw fallback to DocumentURL);
  the extracted producer must redact-or-refuse credential-bearing record
  IDs — open `#4047` cutover item, pinned by `source_parity_8865_test.go`.
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

### First-target cutover observation (2026-10-28, QA)

- Satellite repository `eshu-hq/eshu-collector-sbom-attestation` exists
  (default branch `main`, HEAD `5172564c` observed 2026-10-28); tags
  `v0.1.0`, `v0.1.0-kindfix.1`. No GitHub Release object published.
  (The environment token in the scope, instance, and deploy names below
  is elided as `…`: this repository must not record environment names.
  Full identifiers live in `iac-eks-eshu#401` and on the issue.)
- QA seed image pinned in the `iac-eks-eshu` QA activation
  (`instanceId: sbom-att-cx-…`):
  `ghcr.io/eshu-hq/eshu-collector-sbom-attestation@sha256:c5c709d4a9ebacd5bab3589a6b267496805a2f98d577bd0b50dd5ea3efce8936`
  (comment: `v0.1.0-kindfix.1`, linux/amd64, binary at
  `/usr/local/bin/collector`). First reviewed `configured_source` target
  merged as `iac-eks-eshu#401` (merged 2026-10-28T03:10:42Z): host scope
  `sbom-attestation://…/sbom-fixture-cyclonedx`, single CycloneDX
  fixture target.
- Live QA truth observed read-only 2026-10-28: worker deploy
  `eshu-cx-sbom-att-cx-…` `1/1` Available; 2 `completed` claims on the
  fixture scope (03:13:05Z, 03:33:51Z); 28 `fact_records` with
  `source_system='sbom_attestation'` in 24h (`max(observed_at)`
  2026-10-28T03:33:54Z): `sbom.document` 2, `sbom.component` 10,
  `sbom.dependency_relationship` 6, `sbom.external_reference` 2,
  `sbom.warning` 6, `reducer_sbom_attestation_attachment` 2. Older
  targetless-scope claims (`sbom_attestation:…`) fail terminal
  `invalid_config` — the designed fail-closed, with no facts admitted.
- Extracted-path read-surface agreement observed read-only 2026-10-28
  (late-morning window; claims through 10:00:42Z, generations through
  10:30:44Z): newest 3 `workflow_claims` for `sbom_attestation`
  `completed` 10:00:42Z / 09:30:42Z / 09:00:42Z (instance
  `sbom-att-cx-…`); 210 `fact_records` on scope
  `sbom-attestation://…/sbom-fixture-cyclonedx`
  (`max(observed_at)` 2026-10-28T10:00:43Z; 15 generations × 14 facts at
  check start, 16 at end). Latest-then generation:
  `sbom.document` 1, `sbom.component` 5,
  `sbom.dependency_relationship` 3, `sbom.external_reference` 1,
  `sbom.warning` 3, `reducer_sbom_attestation_attachment` 1 with
  `attachment_status=attached_parse_only`, `artifact_kind=sbom`,
  `attachment_scope=parse_only_unanchored`, `component_count=5`.
- HTTP API `AGREE`: `GET
  /api/v0/supply-chain/sbom-attestations/attachments/count` returned
  HTTP 200 `total_attachments=1`, `by_artifact_kind={sbom:1}`,
  `by_attachment_status={attached_parse_only:1}`; `GET .../attachments`
  returned HTTP 200 `count=1` with all seven compared fields
  (`attachment_status`, `subject_digest`, `document_id`,
  `document_digest`, `artifact_kind`, `attachment_scope`,
  `component_count`) identical to the `fact_records` reducer row, both
  scoped by the `document_id` anchor (the filter requires one anchor per
  `SBOMAttestationAttachmentFilter.HasScope`; there is no `scope_id`
  filter). The first list attempt returned HTTP 503
  (`database read temporarily unavailable`, 2.0s guarded-reader stage
  timeout in `eshu-api` logs); the immediate retry returned 200.
- MCP `AGREE`: `list_sbom_attestation_attachments` (`document_id`
  anchor, `limit 10`) returned HTTP 200 with 1 result; status, digests,
  IDs, artifact kind, scope, `component_count` (5), and `parse_status`
  identical to `fact_records` and HTTP API substance; truth envelope
  `exact/production/semantic_facts/fresh`. The API-vs-MCP
  `attachment_id` differs only by the 30-minute generation rollover
  mid-check (API row = 10:00:43 generation, MCP row = 10:30:44
  generation; both `fact_id`s exist in `fact_records`).
- Still `NOT_CHECKED` on the extracted path: reducer projection beyond
  the attachment row, canonical Neo4j graph projection, and the MCP
  `count_sbom_attestation_attachments` /
  `get_sbom_attestation_attachment_inventory` tools as individually
  live-called surfaces (their route mapping is unit-covered in
  `go/internal/mcp/supply/chain/evidence/routes_test.go`; the
  underlying HTTP `/count` route returned 200 above). Rollback
  rehearsal is open.

## 7. Migration stage

`planned` (advisory diagnostic reads `keep_in_tree`; the diagnostic records evidence and grants no cutover authority). First-target
deployment observed in the owner-designated QA environment (see §6):
satellite repository, tags,
digest-pinned seed image, merged first-target activation, completed
fixture-scope claims with 210 facts, and HTTP API + MCP read-surface
agreement with `fact_records` on the extracted path (see §6). Still not
`ready_for_cutover`: canonical graph projection agreement on the
extracted path is `NOT_CHECKED`, rollback rehearsal is open, and the
public SDK surface is owned by
`#8851`. Roadmap owners: `#6707` inventory, `#6708` SDK/fact-schema,
`#6709` producer delegation, `#6710` template and compatibility harness,
`#4047` per-collector cutover gate.

## 8. `#8865` acceptance sweep at `origin/main`

| Acceptance item | State |
| --- | --- |
| Named owners, production target, configuration source | **Partial** — owner-confirmed on the issue; QA worker and claims observed read-only from this machine 2026-10-28 (deploy `1/1`, §6); configuration source is the QA activation in `iac-eks-eshu` (`instanceId: sbom-att-cx-…`, full identifiers in `iac-eks-eshu#401`) |
| Census, kinds, consumers, state, protocols, artifacts, stage | **Met in PREP** by this document; artifacts/stage updated with the 2026-10-28 observation (§6) |
| Source parity on representative + hostile inputs | **Met in-tree side** by `runtime/source_parity_8865_test.go` + `runtime/testdata/8865-parity-sbom-attestation.json` (AC3) |
| Extracted-path admitted facts, projection, graph, API, MCP truth | **Partial (admission + read-surface agreement)** — completed fixture-scope claims with 210 facts observed 2026-10-28 (§6); HTTP API (`/attachments`, `/count`) and MCP (`list_sbom_attestation_attachments`) agree with `fact_records` (§6); reducer projection beyond the attachment row and canonical graph agreement `NOT_CHECKED`; `#4047` gate still required |
| Retry, duplicates, ordering, cancellation, revoked authority, stale claims, partial/ambiguous failures | **Open** — duplicates + malformed + grant-free delta pinned in-tree; targetless-scope fail-closed terminals observed live (`invalid_config`, no facts); revocation/staleness need the extracted path |
| Bounded resources, no regression, health/readiness/freshness/telemetry | **Open** — in-tree no-regression evidence recorded in `runtime/README.md` and `document/README.md`; first-target fact freshness observed 2026-10-28 (§6); extracted-path proof outstanding |
| Credentials, provenance, vuln response, oldest/newest pairs | **Partial** — credential handling and provenance recorded here; record-ID redact-or-refuse is an open `#4047` cutover item (verbatim carry pinned); version pairs need `#6710` + `#8851` |
| Independent build/release/deploy on public contracts | **Partial** — satellite repo, tags, digest-pinned QA seed image, and merged first-target activation observed (§6); SDK pins verified (see [SHIM_MAP](SHIM_MAP.md)); template outstanding |
| Rollback rehearsal | **Draft recorded** ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)); rehearsal **open** |
| Review, local proof, stable CI, removal after acceptance | **Open** — local proof in this lane's commit; removal explicitly **not** in PREP |

## 9. Prerequisites for cutover

1. Published SDK + fact-schema modules (`#6708`, `#8851` Codex lane) at
   the coordinates pinned in [SHIM_MAP](SHIM_MAP.md) (currently collector
   `v0.1.0-rc.5`, factschema `v0.1.0-rc.10`). Symbol sufficiency was
   verified at `v0.1.0-rc.1` (SHIM_MAP §6); each later bump re-proves it by
   building this lane green, and the pin-parity guard
   (`runtime/shim_pin_8865_test.go`) fails closed on undocumented drift.
2. Producer-record reconciliation for the former 5-vs-8 kind discrepancy
   (§2, widened to all 8 in the producer-record lane) and first-party
   producer delegation live (`#6709`); grant recorded in
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
3. Collector repository from the `#6710` template with the compatibility
   harness matrix (newest collector × oldest core and reverse).
4. Parity proof on representative + hostile inputs via the fixture pack in
   `runtime/testdata/8865-parity-sbom-attestation.json` and
   `runtime/source_parity_8865_test.go` (AC3, in-tree side).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)).
