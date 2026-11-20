# SupplyChainReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. SupplyChain Wave-3 core pin record, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). The pin itself is the
D6 family mint (`SupplyChainReadAdmission` v0 in
go/internal/query/supply/chain/supply_read_admission.go); this record
mints the missing tier-b contracts side core-side. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the legs below.

## Shape

Version: `SupplyChainReadAdmissionContractVersion = "v0"`
(go/internal/query/supply/chain/supply_read_admission.go).

Exactly the 23 embedded narrow family reads, sealed — no suppression
mutation, no shared graph/content port, no snapshot/ledger surface:

| # | Method | Embedded port | Serves |
|---|--------|---------------|--------|
| 1 | `ListSupplyChainImpactFindings(context.Context, impact.FindingFilter) ([]impact.FindingRow, error)` | `impact.FindingStore` | `GET /api/v0/supply-chain/impact/findings` (oracle `paths/supply/chain/impact_findings.go`: 200/403/503/504) |
| 2 | `CountSupplyChainImpactFindings(context.Context, impact.AggregateFilter) (impact.AggregateCount, error)` | `impact.AggregateStore` | `GET /api/v0/supply-chain/impact/findings/count` (oracle `paths/supply/chain/impact_aggregate.go`: 200/403/503/504) |
| 3 | `SupplyChainImpactInventory(context.Context, impact.AggregateFilter, impact.InventoryDimension, int, int) ([]impact.InventoryRow, error)` | `impact.AggregateStore` | `GET /api/v0/supply-chain/impact/inventory` (same fragment: 200/403/503/504) |
| 4 | `SupplyChainImpactGroupedInventory(context.Context, impact.AggregateFilter, impact.GroupedInventoryDimension, int, int) (impact.GroupedInventoryPage, error)` | `impact.GroupedInventoryStore` | `GET /api/v0/supply-chain/impact/inventory?group_by=...` facet (same fragment: 200/403/503/504) |
| 5 | `ExplainSupplyChainImpact(context.Context, packet.ExplanationFilter) (impact.ExplanationRow, error)` | `impact.ExplanationStore` | `GET /api/v0/supply-chain/impact/explain` (oracle `paths/supply/chain/impact_explain.go`: 200/400/403/503/504) |
| 6 | `ReadSupplyChainImpactReadiness(context.Context, impact.ReadinessQuery) (packet.ReadinessSnapshot, error)` | `impact.ReadinessStore` | Readiness envelope inside the findings/explain/packet legs (no standalone route) |
| 7 | `ListContainerImageIdentities(context.Context, ContainerImageIdentityFilter) ([]ContainerImageIdentityRow, error)` | `ContainerImageIdentityStore` | `GET /api/v0/supply-chain/container-images/identities` (oracle `paths/supply/chain/container_images.go`: 200/403/503/504) |
| 8 | `CountContainerImageIdentities(context.Context, ContainerImageIdentityAggregateFilter) (ContainerImageIdentityAggregateCount, error)` | `ContainerImageIdentityAggregateStore` | `GET /api/v0/supply-chain/container-images/identities/count` (oracle `paths/supply/chain/container_image_identity_aggregate.go`: 200/403/503/504) |
| 9 | `ContainerImageIdentityInventory(context.Context, ContainerImageIdentityAggregateFilter, ContainerImageIdentityInventoryDimension, int, int) ([]ContainerImageIdentityInventoryRow, error)` | `ContainerImageIdentityAggregateStore` | `GET /api/v0/supply-chain/container-images/identities/inventory` (same fragment: 200/403/503/504) |
| 10 | `ListSBOMAttestationAttachments(context.Context, SBOMAttestationAttachmentFilter) (SBOMAttestationAttachmentPage, error)` | `SBOMAttestationAttachmentStore` | `GET /api/v0/supply-chain/sbom-attestations/attachments` (oracle `paths/supply/chain/sbom_attestations.go`: 200/403/503/504) |
| 11 | `CountSBOMAttestationAttachments(context.Context, SBOMAttestationAttachmentAggregateFilter) (SBOMAttestationAttachmentAggregateCount, error)` | `SBOMAttestationAttachmentAggregateStore` | `GET /api/v0/supply-chain/sbom-attestations/attachments/count` (oracle `paths/supply/chain/sbom_attestation_attachment_aggregate.go`: 200/403/503/504) |
| 12 | `SBOMAttestationAttachmentInventory(context.Context, SBOMAttestationAttachmentAggregateFilter, SBOMAttestationAttachmentInventoryDimension, int, int) ([]SBOMAttestationAttachmentInventoryRow, error)` | `SBOMAttestationAttachmentAggregateStore` | `GET /api/v0/supply-chain/sbom-attestations/attachments/inventory` (same fragment: 200/403/503/504) |
| 13 | `ListSecurityAlertReconciliations(context.Context, SecurityAlertReconciliationFilter) ([]SecurityAlertReconciliationRow, error)` | `SecurityAlertReconciliationStore` | `GET /api/v0/supply-chain/security-alerts/reconciliations` (oracle `paths/supply/chain/security_alerts.go`: 200/403/503/504) |
| 14 | `CountSecurityAlertReconciliations(context.Context, SecurityAlertReconciliationAggregateFilter) (SecurityAlertReconciliationAggregateCount, error)` | `SecurityAlertReconciliationAggregateStore` | `GET /api/v0/supply-chain/security-alerts/reconciliations/count` (oracle `paths/supply/chain/security_alert_aggregate.go`: 200/403/503/504) |
| 15 | `SecurityAlertReconciliationInventory(context.Context, SecurityAlertReconciliationAggregateFilter, SecurityAlertReconciliationInventoryDimension, int, int) ([]SecurityAlertReconciliationInventoryRow, error)` | `SecurityAlertReconciliationAggregateStore` | `GET /api/v0/supply-chain/security-alerts/reconciliations/inventory` (same fragment: 200/403/503/504) |
| 16 | `SecurityAlertCollectorConfigurations(context.Context) ([]string, error)` | `SecurityAlertCollectionStateStore` | Collection-state facet inside the reconciliations legs (empty-answer reads) |
| 17 | `SecurityAlertTargetStates(context.Context, []string, []string) ([]SecurityAlertTargetState, error)` | `SecurityAlertCollectionStateStore` | Same collection-state facet as row 16 |
| 18 | `SecurityAlertReconciliationQueueStates(context.Context, []string) ([]SecurityAlertReconciliationQueueState, error)` | `SecurityAlertCollectionStateStore` | Same collection-state facet as row 16 |
| 19 | `ListAdvisoryCatalog(context.Context, advisory.CatalogFilter) (advisory.CatalogPage, error)` | `advisory.CatalogStore` | `GET /api/v0/supply-chain/advisories` (oracle `paths/supply/chain/advisory_catalog.go`: 200/400/501/503) |
| 20 | `ListAdvisoryEvidence(context.Context, advisory.EvidenceFilter) ([]advisory.EvidenceRow, error)` | `advisory.EvidenceStore` | `GET /api/v0/supply-chain/advisories/evidence` (oracle `paths/supply/chain/advisory_evidence.go`: 200/400/403/404/409/503/504) and `GET /api/v0/supply-chain/vulnerabilities/{advisory_id}` (same fragment: 200/400/404/501/503) |
| 21 | `CurrentAuthorizedCloudResourceUIDs(context.Context, []string, bool, []string, []string) ([]string, error)` | `CloudResourceCurrentInventoryFilter` | Runtime-evidence gating inside the finding legs (no standalone route) |
| 22 | `CurrentAuthorizedCloudResourcesByDigest(context.Context, []string, bool, []string, []string) ([]CloudResourceRuntimeDigestMatch, error)` | `CloudResourceRuntimeDigestResolver` | Same gating as row 21 |
| 23 | `CurrentAuthorizedKubernetesRuntimeWorkloads(context.Context, []KubernetesRuntimeCandidate, bool, []string, []string) ([]KubernetesRuntimeWorkloadMatch, error)` | `KubernetesWorkloadCurrentInventoryFilter` | Same gating as row 21 |

The vulnerability-scanner contract leg
(`GET /api/v0/supply-chain/vulnerability-scanner/contract`, oracle
`paths/supply/chain/vulnerability_scanner_contract.go`: 200) is static
— no store read, no pin method.

Transport: the legs stay on the pin-assignable narrow handler fields
(`chain.Handler`); the root query-package composer proves the
production stores jointly satisfy the pin (see
go/internal/query/w5_supply_chain_impact_packet_read_admission_mirror.go
for the packet-side production mirror). No parallel wiring.

Evidence (not enforcement): the pin-satisfying witness
(`supplyChainReadAdmissionWitness` in
`supply_read_admission_contract_test.go`) implements exactly the 23
pinned reads over zero values, and the real-mux route tests
(`route_registration_test.go`: `TestListSBOMAttachmentsRoute`,
`TestListAdvisoryCatalogRoute`, `TestExplainImpactRoute`,
`TestListContainerImageIdentitiesRoute`,
`TestListSecurityAlertReconciliationsRoute`, …) prove every mounted
route answers through `chain.Handler.Mount` registrations.

Enforcement (all in go/internal/query/supply/chain):

- `supply_read_admission_contract_test.go` — frozen method-set test
  plus version pin
  (`TestSupplyChainReadAdmissionContractIsVersioned`), witness
  (`supplyChainReadAdmissionWitness` + satisfier assertion),
  seeded-drift RED proof
  (`TestSupplyChainReadAdmissionSeededDriftIsDetected` — a planted
  suppression write must not match the frozen set).
- `supply_read_admission_contract_test.go` — seal
  (`TestSupplyChainReadAdmissionWireShapesAreSealed`) — exactly the 23
  embedded reads, no suppression/mutation/shared-port surface.
- Route proof through the real mux: the `route_registration_test.go`
  `*Route` tests above (a 404 means the route is not registered).
- Compile-time satisfier assertion in the contract test:
  `var _ SupplyChainReadAdmission = supplyChainReadAdmissionWitness{}`.

## Satellite consumption

Depend on the `SupplyChainReadAdmission` port; satisfy it with the
existing family stores. The legs already exercise exactly the pinned
methods through the pin-assignable narrow handler fields — the
composer proves the production stores jointly serve the pin.

## W8a disclosed-absence posture

All 18 SupplyChain read rows are natively mounted in-tree
(`chain.Handler.Mount` in go/internal/query/supply/chain/handler.go
plus the `supplyChainImpactAggregateRoutes`,
`securityAlertReconciliationAggregateRoutes`,
`containerImageIdentityAggregateRoutes`, and
`sbomAttestationAttachmentAggregateRoutes` helpers) and their OpenAPI
fragments stay core-side (oracle `paths/supply/chain/*` and
`paths/evidence/investigations.go` with the codes in the table above)
— disclosed here, not silently dropped. No `contracts/upstream/*`
stub covers these legs: the wire-shape generator snapshots forwarded
relay spellings only, and no SupplyChain leg has one. The mount +
core-side fragments + spec-key list above is the complete termination
record for the 18 read legs.

## Explicitly OUT (not widened by this pin)

- `POST /api/v0/supply-chain/impact/suppressions` — the sole bounded
  mutation. It stays the Decision 2 tier-C survivor via
  `SuppressionAdmission` (already pinned,
  `suppression_admission_contract_test.go`), never a read termination
  here.
- The impact-packet composition surface (`SupplyChainImpactPacketReadAdmission`
  v0 W5f packet pin, `ImpactPacketResponder`, packet composers) — the
  packet route admits its explanation/readiness reads through the
  family stores pinned here, but the packet-scoped re-declaration and
  the shared selector closure stay covered by the W5f pin, not widened
  here.
- Neo4j `GraphQuery`, `ContentStore`, and `service.EvidenceReader`
  (selector resolution, runtime probes, packet composition) — shared
  cross-family ports; pinning them here would be a blanket pin, which
  the arbiter ruling forbids.
- `querycontract.CollectorListReadinessStore` (shared root readiness
  contract) — the family-local copies stay behavior-identical to root
  and are not frozen here.
- The optional provider-repository-scope lookup facet
  (`securityAlertProviderRepositoryScopeStore`) — satisfied by the
  same structs behind the two SecurityAlert ports, not separately
  frozen.

## Rollback

Revert this pin plus the query-package composer. The legs stay on the
pin-assignable narrow fields (they exercise exactly the pinned
methods) while the composer proves the stores jointly satisfy the
pin — rollback removes the composer only and moves no traffic.
