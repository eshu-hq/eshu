# cloudinventory

## Purpose

Admits provider cloud-inventory source facts into the shared canonical
`cloud_resource_uid` keyspace and persists them as reducer-owned canonical
CloudResource read-model facts. One domain, one family:

- `DomainCloudInventoryAdmission` (#1997, #1998): consumes `aws_resource`,
  `gcp_cloud_resource`, and `azure_cloud_resource` source facts for one scope
  generation, resolves each record's provider raw identity (AWS ARN, GCP Cloud
  Asset Inventory full resource name, Azure ARM resource id) into one stable
  uid, and writes one `reducer_cloud_resource_identity` fact per admitted
  resource. Declared, applied, and observed layers stay distinct so an observed
  fact never demotes declared truth. Tag (`azure_tag_observation`),
  identity-policy (`azure_identity_observation`), and resource-change freshness
  evidence attach onto the admitted resource sharing their uid; none of them
  admits resources on its own. Blank, malformed, ambiguous, and unsupported
  identities are counted, never fabricated.

This package moved out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the additive domain definition and the admission handler,
the admission-decision writer path, the canonical writer (with its stable fact
key, deterministic fact id, and canonical payload builders), the
fold/normalize/bound helpers for all four evidence kinds, and the
`SourceLayer` / `ManagementOrigin` vocabularies.

It does **not** own provider raw-identity resolution
(`internal/correlation/cloudinventory`), shared admission-decision vocabulary
(`reducer/admissiondecision`), the fact batch insert (`reducer/factwrite`),
generation freshness (`reducer/contract`), or telemetry instruments
(`internal/telemetry`). Registration stays in the reducer root
(`defaults_additive_domains_secrets_drift.go`), the domain constant alias
stays in the root (`intent_domain_platform.go`), the evidence loaders' Postgres
implementations live under `internal/storage/postgres`, and the command wiring
lives under `cmd/reducer`.

## Exported surface

- `CloudInventoryAdmissionDomainDefinition` — the root admission registry
  (`defaults_additive_domains_secrets_drift.go`)
- `CloudInventoryAdmissionHandler` — the root registry above
- `PostgresCloudInventoryAdmissionWriter` — `cmd/reducer`; `defaults.go`
  declares `DefaultHandlers.CloudInventoryAdmissionWriter` with this type
  directly — no root compat file
- `CloudInventoryRecord`, `AdmittedCloudResource`,
  `CloudInventoryAdmissionWrite`, `CloudInventoryAdmissionWriteResult`,
  `CloudInventoryAdmissionSummary` — `internal/storage/postgres` loaders and
  the writer payload path
- `CloudInventoryEvidenceLoader`, `CloudInventoryAdmissionWriter`,
  `CloudTagEvidenceLoader`, `CloudIdentityPolicyEvidenceLoader`,
  `CloudResourceChangeEvidenceLoader` — root `CloudInventoryHandlers` group
  (`defaults_handlers.go`), `cmd/reducer` wiring, and Postgres loaders
- `CloudTagEvidenceRecord`, `CloudIdentityPolicyEvidenceRecord`,
  `CloudIdentityPolicyEvidence`, `CloudResourceChangeEvidenceRecord`,
  `CloudResourceChangeEvidence` — Postgres loaders and payload builders
- `SourceLayer` (`SourceLayerDeclared` / `SourceLayerApplied` /
  `SourceLayerObserved`), `ManagementOrigin` (`ManagementOriginDeclared` /
  `ManagementOriginApplied` / `ManagementOriginObserved`) — Postgres loaders
  and handler tests

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/admissiondecision`, `reducer/factwrite`,
`internal/correlation/cloudinventory`, `internal/facts`, `internal/telemetry`,
`internal/truth`. Never `internal/reducer`, never a sibling family package.

## Telemetry

One package instrument: `eshu_dp_cloud_inventory_admissions_total`
(dimensioned by `provider` for admitted resources, by `outcome` alone for
`ambiguous` / `unsupported` / `unresolved` since a non-admitted record may
carry an unsupported provider token). Every name verified against
`internal/telemetry/instruments.go`. The non-admitted buckets are
map-driven through `addOutcomeCount`, which returns early on zero, so a quiet
generation emits no per-reason series; the admission summary
(`admitted/ambiguous/unsupported/unresolved/canonical_writes`) in the
`EvidenceSummary` is the always-present record. Malformed provider identities
never reach quarantine as `input_invalid` — they are counted outcomes, not
malformed facts.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk in the moved production files is a package clause, an
import requalification, or an identifier requalification: symbols the reducer
root exposed as one-line forwarders (`workloadIdentityExecer` /
`reducerFactRow` / `reducerBatchInsertFacts` / `reducerWriterNow` /
`reducerFactCollectorKind` → `factwrite.*`, `Intent` / `Result` /
`ResultStatusSucceeded` / `ResultStatusSuperseded` / `DomainDefinition` /
`OwnershipShape` / `GenerationFreshnessCheck` /
`DomainCloudInventoryAdmission` → `reducercontract.*`) are now called on the
leaf package directly; key sorting goes through the shared
`payloadcore.SortedKeys` (the root `sortedKeys` forwarder in
`candidate_loader.go` stays for `candidate_loader.go`,
`code_import_repo_edge.go`, and `package_consumption_repo_edge.go`). The
`correlation/cloudinventory`, `admissiondecision`, `facts`, `telemetry`, and
`truth` calls were already leaf-qualified and are untouched. No handler
branch, fold rule, attach rule, bound/cap, dedupe key, payload shape,
idempotency key, or generation gate changed.
`go test ./internal/reducer/cloudinventory -count=1` passes with the moved
test suite unchanged in assertion content (only package clause, import paths,
leaf requalification, and unexported symbol duplication for the package
boundary).

No-Observability-Change: the move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, or metric label; the
shared admission counter keeps the same name, labels, and key set at the new
import path.
