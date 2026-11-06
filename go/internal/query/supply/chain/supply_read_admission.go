// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// SupplyChainReadAdmissionContractVersion versions the #9669 D6
// admission-contract shape for the SupplyChain family reads the
// supply-chain endpoints admit through the family reader ports.
// Canonical reads persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned
// port, which the core-owned Postgres stores satisfy. A breaking change
// to the read shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers
// can pin the shape.
const SupplyChainReadAdmissionContractVersion = "v0"

// SupplyChainReadAdmission is the versioned admission-contract port the API
// handler depends on instead of direct store handles. It embeds the
// family-owned reader ports so the boundary is identity-verified by the
// frozen contract test rather than a parallel hand-maintained shape; it will
// back the eshu-api extraction handoff for this family. The seam is read-only
// on purpose: every embedded port carries bounded list/count/inventory reads
// and no mutation surface, so a dest consumer implementing only this contract
// can serve supply-chain reads but cannot acquire a write it did not bargain
// for through it.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter D-wave
// ruling on #9669). The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid.
//
// In scope (the 18 SupplyChain READ routes admit through exactly these
// family-owned ports):
//
//	list impact findings (+count/inventory) through impact.FindingStore,
//	impact.AggregateStore, and impact.GroupedInventoryStore; the impact
//	explain and investigation-packet reads through impact.ExplanationStore;
//	the vulnerability-scanner contract route is static (no store port).
//	list SBOM/attestation attachments (+count/inventory) through
//	SBOMAttestationAttachmentStore and
//	SBOMAttestationAttachmentAggregateStore.
//	list container-image identities (+count/inventory) through
//	ContainerImageIdentityStore and ContainerImageIdentityAggregateStore.
//	list security-alert reconciliations (+count/inventory) through
//	SecurityAlertReconciliationStore and
//	SecurityAlertReconciliationAggregateStore, with empty-answer
//	collection-state reads through SecurityAlertCollectionStateStore.
//	advisory catalog and evidence reads plus the vulnerability-detail read
//	through advisory.CatalogStore and advisory.EvidenceStore.
//	readiness envelopes through impact.ReadinessStore.
//	runtime-evidence gating inside the finding reads through
//	CloudResourceCurrentInventoryFilter,
//	CloudResourceRuntimeDigestResolver, and
//	KubernetesWorkloadCurrentInventoryFilter.
//
// Satellite-side proof obligation (arbiter Decision 2): parity must include
// suppression-state filtering or the split is unproven. The finding reads
// consult suppression state only inside reader-store SQL — FindingFilter
// SuppressionState/IncludeSuppressed, the reconciliation provider-state and
// reconciliation-status filters — never via the mutation port below. The
// satellite parity proof for this split must exercise those filters; this pin
// freezes the port shapes, not the proof.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - The suppressions mutation row stays out: it is the Never-satellite
//     survivor via SuppressionAdmission (already pinned), not a read port.
//   - Neo4j GraphQuery, Content ContentStore, and service.EvidenceReader are
//     shared cross-family ports (selector resolution, runtime probes, and
//     packet composition also read through them). Pinning them here would be
//     a blanket pin, which the arbiter ruling forbids.
//   - querycontract.CollectorListReadinessStore is the shared root readiness
//     contract; the family-local copies stay behavior-identical to root and
//     are not frozen here.
//   - ImpactPacketResponder composes and writes the packet envelope in root;
//     it is a writer responder, not a reader port.
//   - The optional provider-repository-scope lookup facet
//     (securityAlertProviderRepositoryScopeStore) names no separate port: it
//     is satisfied by the same structs behind the two SecurityAlert ports and
//     is not separately frozen. Exporting that seam to freeze it would widen
//     this pin; a future wave may do so deliberately.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type SupplyChainReadAdmission interface {
	SBOMAttestationAttachmentStore
	SBOMAttestationAttachmentAggregateStore
	ContainerImageIdentityStore
	ContainerImageIdentityAggregateStore
	SecurityAlertReconciliationStore
	SecurityAlertReconciliationAggregateStore
	SecurityAlertCollectionStateStore
	CloudResourceCurrentInventoryFilter
	CloudResourceRuntimeDigestResolver
	KubernetesWorkloadCurrentInventoryFilter
	impact.FindingStore
	impact.AggregateStore
	impact.GroupedInventoryStore
	impact.ExplanationStore
	impact.ReadinessStore
	advisory.EvidenceStore
	advisory.CatalogStore
}
