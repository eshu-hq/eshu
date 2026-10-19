// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the correlation families (cicdrun, crossrepo, supplychain)
// (issue #6061). It merges the per-family *_compat.go files listed below
// with no behavior change: every alias and forwarder is preserved
// byte-identical under its stanza marker. A family move adds a stanza
// to the matching bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md). Each entry is deleted
// once its last caller has moved; see the importer-migration child issue.
// The service_catalog_correlation and sbom_attestation_attachment stanzas
// below were fully retired once their last callers named [servicecatalog]
// and [sbomattest] directly (#6626 tail bucket 1).
//
// Stanzas merged here:
//   - ci_cd_run_correlation_compat.go
//   - cross_repo_compat.go
//   - supply_chain_impact + supply_chain_suppression (family move; no prior compat file).
//   - shared-projection intent row + unroutable-write (H5 root-remnant
//     fold, issue #6061; folded from shared_projection_unroutable.go)
//   - graph-projection phase repair (H5 root-remnant fold, issue #6061;
//     folded from graph_projection_phase_repair.go and
//     graph_projection_phase_repair_runner.go)

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/intents/phase/repair"
	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// Stanza: ci_cd_run_correlation_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the CI/CD run
// correlation domain that moved to [cicdrun] (issue #6061). Reducer-root call
// sites and the external packages that name these types keep their current
// spelling; each entry is deleted once its last caller has moved into a
// family subpackage.

// The CICDRunCorrelation outcome values forward to their [cicdrun] equivalents.
const (
	CICDRunCorrelationExact   = cicdrun.CICDRunCorrelationExact
	CICDRunCorrelationDerived = cicdrun.CICDRunCorrelationDerived
)

// CICDRunCorrelationDecision forwards to [cicdrun.CICDRunCorrelationDecision].
type CICDRunCorrelationDecision = cicdrun.CICDRunCorrelationDecision

// CICDRunCorrelationWrite forwards to [cicdrun.CICDRunCorrelationWrite].
type CICDRunCorrelationWrite = cicdrun.CICDRunCorrelationWrite

// CICDRunCorrelationWriteResult forwards to
// [cicdrun.CICDRunCorrelationWriteResult].
type CICDRunCorrelationWriteResult = cicdrun.CICDRunCorrelationWriteResult

// CICDRunCorrelationWriter forwards to [cicdrun.CICDRunCorrelationWriter].
type CICDRunCorrelationWriter = cicdrun.CICDRunCorrelationWriter

// CICDRunCorrelationHandler forwards to [cicdrun.CICDRunCorrelationHandler].
type CICDRunCorrelationHandler = cicdrun.CICDRunCorrelationHandler

// PostgresCICDRunCorrelationWriter forwards to
// [cicdrun.PostgresCICDRunCorrelationWriter].
type PostgresCICDRunCorrelationWriter = cicdrun.PostgresCICDRunCorrelationWriter

// cicdWorkflowImageBuiltFromEvidenceSource forwards to
// [cicdrun.CICDWorkflowImageBuiltFromEvidenceSource].
const cicdWorkflowImageBuiltFromEvidenceSource = cicdrun.CICDWorkflowImageBuiltFromEvidenceSource

// Stanza: cross_repo_compat.go (merged; do not recreate this file).
// The cross-repo resolution family moved to [crossrepo] under issue #6061.
// These aliases keep the reducer root's own callers -- and the packages that
// name them through this package (cmd/reducer, internal/ifa/materializededges,
// internal/storage/cypher) -- compiling against the same spellings. The
// dependency runs root -> family only; the family never imports this package.

// CrossRepoEvidenceSource is the evidence_source the cross-repo resolver stamps
// on every edge it writes. Alias for [crossrepo.CrossRepoEvidenceSource].
const CrossRepoEvidenceSource = crossrepo.CrossRepoEvidenceSource

// EvidenceFactLoader loads persisted evidence facts for a generation.
// Alias for [crossrepo.EvidenceFactLoader].
type EvidenceFactLoader = crossrepo.EvidenceFactLoader

// AssertionLoader loads relationship assertions.
// Alias for [crossrepo.AssertionLoader].
type AssertionLoader = crossrepo.AssertionLoader

// ResolutionPersister persists resolution outputs and activates the generation.
// Alias for [crossrepo.ResolutionPersister].
type ResolutionPersister = crossrepo.ResolutionPersister

// ResolvedUpsertResult reports what one UpsertResolved call actually wrote.
// Alias for [crossrepo.ResolvedUpsertResult].
type ResolvedUpsertResult = crossrepo.ResolvedUpsertResult

// CompletionWakeResult reports what one completion wake transaction did.
// Alias for [crossrepo.CompletionWakeResult].
type CompletionWakeResult = crossrepo.CompletionWakeResult

// DeployableUnitCorrelationWaker reopens the deployable_unit_correlation
// items whose recorded input no longer holds after a resolution pass.
// Alias for [crossrepo.DeployableUnitCorrelationWaker].
type DeployableUnitCorrelationWaker = crossrepo.DeployableUnitCorrelationWaker

// ScopeRepositoryReader lists a scope generation's git repository IDs.
// Alias for [crossrepo.ScopeRepositoryReader].
type ScopeRepositoryReader = crossrepo.ScopeRepositoryReader

// RepoDependencyIntentWriter persists durable repo-dependency projection
// intents. Alias for [crossrepo.RepoDependencyIntentWriter].
type RepoDependencyIntentWriter = crossrepo.RepoDependencyIntentWriter

// CrossRepoRelationshipHandler resolves cross-repository relationships from
// persisted evidence facts. Alias for [crossrepo.CrossRepoRelationshipHandler].
type CrossRepoRelationshipHandler = crossrepo.CrossRepoRelationshipHandler

// Stanza sbom_attestation_attachment_compat.go: fully retired (#6626 tail bucket 1).
// Every caller names [sbomattest] directly; the notes below are ledger.

// MaxSBOMAttachmentComponentEvidenceRows, ComponentEvidence,
// ComponentEvidenceLess, and ComponentEvidenceTupleEqual were retired once
// their last callers moved to [sbomattest] (#6626 batch 2).

// sbomAttestationAttachmentFactKind is not aliased in root any more: the moved
// sbomattest family names [reducercontract.SBOMAttestationAttachmentFactKind]
// directly, and the intent.go alias this comment used to point at was removed
// once the last in-root caller moved (#6061). containerImageIdentityFactKind in
// intent.go keeps the same direct-from-contract shape (#6431).

// Stanza: supply_chain_impact + supply_chain_suppression (family move; no prior compat file).
// This file is the transitional compatibility surface for the supply-chain
// impact + suppression family that moved to [supplychaincore] (issue #6061).
// External packages that name these types (cmd/reducer's writer construction,
// internal/storage/postgres' loaders, internal/replay/costcounting's fixture
// findings) keep their current reducer.X spelling; each entry is deleted once
// its last caller has moved into a family subpackage. The dependency runs
// root -> family only; the family never imports this package.

// SupplyChainImpactFactFilter bounds active evidence loading for one impact
// reducer intent. See [supplychaincore.SupplyChainImpactFactFilter].
type SupplyChainImpactFactFilter = supplychaincore.SupplyChainImpactFactFilter

// SupplyChainImpactFinding is one reducer-owned vulnerability impact finding.
// See [supplychaincore.SupplyChainImpactFinding].
type SupplyChainImpactFinding = supplychaincore.SupplyChainImpactFinding

// SupplyChainImpactAffectedExact means package identity and observed version
// match the affected package evidence exactly. See
// [supplychaincore.SupplyChainImpactAffectedExact].
const SupplyChainImpactAffectedExact = supplychaincore.SupplyChainImpactAffectedExact

// SupplyChainImpactWrite carries findings for durable publication. See
// [supplychaincore.SupplyChainImpactWrite].
type SupplyChainImpactWrite = supplychaincore.SupplyChainImpactWrite

// SupplyChainImpactWriteResult summarizes durable impact publication. See
// [supplychaincore.SupplyChainImpactWriteResult].
type SupplyChainImpactWriteResult = supplychaincore.SupplyChainImpactWriteResult

// SupplyChainImpactHandler publishes vulnerability impact findings without
// turning CVSS, EPSS, or KEV signals into reachability proof. See
// [supplychaincore.SupplyChainImpactHandler].
type SupplyChainImpactHandler = supplychaincore.SupplyChainImpactHandler

// PostgresSupplyChainImpactWriter is the Postgres-backed
// SupplyChainImpactWriter. See
// [supplychaincore.PostgresSupplyChainImpactWriter].
type PostgresSupplyChainImpactWriter = supplychaincore.PostgresSupplyChainImpactWriter

// SupplyChainImpactWriter persists reducer-owned impact findings. See
// [supplychaincore.SupplyChainImpactWriter].
type SupplyChainImpactWriter = supplychaincore.SupplyChainImpactWriter

// SupplyChainImpactFencingTokenIssuer: see [supplychaincore.SupplyChainImpactFencingTokenIssuer].
type SupplyChainImpactFencingTokenIssuer = supplychaincore.SupplyChainImpactFencingTokenIssuer

// SupplyChainImpactWriteSupersededFailureClass: see [supplychaincore.SupplyChainImpactWriteSupersededFailureClass].
const SupplyChainImpactWriteSupersededFailureClass = supplychaincore.SupplyChainImpactWriteSupersededFailureClass

// SupplyChainImpactTx is the transaction one impact write runs in. See
// [supplychaincore.SupplyChainImpactTx].
type SupplyChainImpactTx = supplychaincore.SupplyChainImpactTx

// SupplyChainImpactBeginner opens the transaction for one impact write. See
// [supplychaincore.SupplyChainImpactBeginner].
type SupplyChainImpactBeginner = supplychaincore.SupplyChainImpactBeginner

// SupplyChainImpactWinnersMaintainer keeps the
// supply_chain_impact_canonical_winners read model reconciled with the active
// impact facts. See [supplychaincore.SupplyChainImpactWinnersMaintainer].
type SupplyChainImpactWinnersMaintainer = supplychaincore.SupplyChainImpactWinnersMaintainer

// JVMReachabilityFactFilter bounds active JVM reachability evidence loading.
// See [supplychaincore.JVMReachabilityFactFilter].
type JVMReachabilityFactFilter = supplychaincore.JVMReachabilityFactFilter

// Stanza: shared-projection intent row and unroutable-write (H5
// root-remnant fold, issue #6061). Writer.Write MUST fail the owning cycle
// and be idempotent (ON CONFLICT DO NOTHING keyed on intent id).
// SharedProjectionIntentRow was retired when its last callers named
// sharedintent.Row directly (#6626 batch 6 bucket 5).
type (
	SharedProjectionIntentInput   = sharedintent.Input
	SharedProjectionAcceptanceKey = sharedintent.AcceptanceKey
	SharedProjectionUnroutableRow = sharedintent.UnroutableRow
	SharedProjectionWriteReport   = sharedintent.WriteReport
	FirstProjectionLookup         = worker.FirstProjectionLookup
)

func BuildSharedProjectionIntent(input SharedProjectionIntentInput) sharedintent.Row {
	return sharedintent.Build(input)
}

func CarriesNoEdge(row sharedintent.Row) bool {
	return sharedintent.CarriesNoEdge(row)
}

// Stanza: graph-projection phase repair (H5 root-remnant fold, issue #6061;
// folded from graph_projection_phase_repair.go and
// graph_projection_phase_repair_runner.go).
type (
	GraphProjectionPhaseRepair         = gpphase.PhaseRepair
	GraphProjectionPhaseRepairQueue    = gpphase.PhaseRepairQueue
	GraphProjectionPhaseRepairerConfig = repair.Config
	GraphProjectionPhaseRepairer       = repair.Repairer
)

func GraphProjectionPhaseRepairsFromStates(states []gpphase.PhaseState, lastError string, enqueuedAt time.Time) []GraphProjectionPhaseRepair {
	return gpphase.PhaseRepairsFromStates(states, lastError, enqueuedAt)
}

// repoWideRetractRefreshPartitionKey forwards to [sharedintent.RepoWideRetractRefreshPartitionKey]
// (H5 root-remnant fold, issue #6061; relocated here from compat_decode.go
// for line-budget headroom).
func repoWideRetractRefreshPartitionKey(domain, repoID string) string {
	return sharedintent.RepoWideRetractRefreshPartitionKey(domain, repoID)
}
