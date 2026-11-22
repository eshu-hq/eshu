// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/reducer/workloadinstance"
)

// WaitingWakeKind is the closed waits_for_kind enum for the reducer waiting
// set-aside (migration 316, #10876 decision 5a). The key shape per kind is
// JSONB so the #10877 corpus-fence narrowing can move from one global key to
// per-item read sets without a schema change.
type WaitingWakeKind string

// Closed waits_for_kind set (#10876 decision 5a).
const (
	// WaitingKindRelationshipGenerationActive waits for the waiter's own
	// relationship generation to activate. Key: {scope_id, generation_id}.
	WaitingKindRelationshipGenerationActive WaitingWakeKind = "relationship_generation_active"
	// WaitingKindPhaseCommitted waits for the producer's canonical-node
	// phase commit. Key: {scope_id, generation_id}; the claim gate and the
	// handler re-check the exact requirement row, so the wake is a hint.
	WaitingKindPhaseCommitted WaitingWakeKind = "phase_committed"
	// WaitingKindGenerationActive waits for the generation to resolve: the
	// projector Ack that activates it, the terminal projector Fail that
	// retires it, or the projector Claim sweep that supersedes it (#11015,
	// probe widened #11016: every scope the sweep scans, empty sweeps,
	// heartbeat supersede, Ack maintenance).
	// Key: {scope_id, generation_id}.
	WaitingKindGenerationActive WaitingWakeKind = "generation_active"
	// WaitingKindProducerTerminal waits for a same-scope producer domain to
	// reach a terminal reducer row. Key: {scope_id, generation_id,
	// producer_domain}; the wake fires only on that producer's terminal Ack.
	WaitingKindProducerTerminal WaitingWakeKind = "producer_domain_terminal"
	// WaitingKindWinnersWatermark waits for the advisory winners maintainer
	// rebuild to pass the wait's enqueue time. Key: {enqueued_at}; the wake
	// is a range reopen, not an equality lookup.
	WaitingKindWinnersWatermark WaitingWakeKind = "winners_watermark"
	// WaitingKindFenceClear waits for an absence fence to empty (value-flow
	// inputs, cloud admission drain). Key: {scope_id, generation_id,
	// domains}; resolved by the completion-event fanout, re-checked by the
	// audit against the live fence predicate.
	WaitingKindFenceClear WaitingWakeKind = "fence_clear"
	// WaitingKindTargetCommitted waits for a named target or evidence commit
	// the waiter cannot cheaply name (foreign-scope evidence and targets).
	// Key: {scope_id, generation_id}; resolved by provider ACKs where the
	// ACK is proven ordered after the commit it certifies, else by the audit
	// terminal-progress fallback and always by the 10-minute backstop.
	WaitingKindTargetCommitted WaitingWakeKind = "target_committed"
)

// Valid reports whether k is one of the seven closed waits_for kinds.
func (k WaitingWakeKind) Valid() bool {
	switch k {
	case WaitingKindRelationshipGenerationActive,
		WaitingKindPhaseCommitted,
		WaitingKindGenerationActive,
		WaitingKindProducerTerminal,
		WaitingKindWinnersWatermark,
		WaitingKindFenceClear,
		WaitingKindTargetCommitted:
		return true
	}
	return false
}

// Waiting wake sites: where the reopening save runs.
const (
	// WaitingWakeSiteInTx means the providing save reopens the wait rows in
	// its own transaction through the shared wake helper.
	WaitingWakeSiteInTx = "in_tx"
	// WaitingWakeSiteCompletion means the retained completion-event fanout
	// owns the wake; the wait row exists for counting, oldest-age, and audit
	// visibility, and the backstop still bounds it.
	WaitingWakeSiteCompletion = "completion_event"
	// WaitingWakeSiteAudit means only the audit and the backstop cover the
	// wait: no single providing save exists, or the ACK ordering is unproven.
	WaitingWakeSiteAudit = "audit"
	// WaitingWakeSiteCounting marks own-gate counting classes: never parked,
	// loud dead letter on budget, existing #7597 wake only.
	WaitingWakeSiteCounting = "counting"
	// WaitingWakeSiteNotAWait marks write-superseded classes: no providing
	// save will ever arrive for that generation, so parking would wait
	// forever. Non-counting retry as today, never a wait row.
	WaitingWakeSiteNotAWait = "not_a_wait"
)

// waitingScopeGenerationKey renders the waits_for_key for the
// scope-generation keyed wait kinds (relationship_generation_active,
// phase_committed, generation_active, target_committed, and the un-narrowed
// producer-terminal waits).
func waitingScopeGenerationKey(scopeID, generationID string) string {
	raw, err := json.Marshal(waitingKey{ScopeID: strings.TrimSpace(scopeID), GenerationID: strings.TrimSpace(generationID)})
	if err != nil {
		return ""
	}
	return string(raw)
}

// WaitingDeferral is one row of the #10876 deferral map (acceptance
// criterion 1): every reason work is put back, what it waits for, and which
// save provides it.
type WaitingDeferral struct {
	// FailureClass is the durable failure_class of the deferred row.
	FailureClass string
	// WaitsForKind is the closed waits_for kind of the parked wait.
	WaitsForKind WaitingWakeKind
	// WakeSite names where the reopening save runs.
	WakeSite string
	// ProvidingSave names the transaction or loop that provides what the
	// wait needs, for operators reading the map.
	ProvidingSave string
	// ProducerDomain, when set, narrows a producer-terminal wait to one
	// producer domain: the park key is {scope_id, generation_id,
	// producer_domain} of the waiter.
	ProducerDomain string
	// Keyspace and Phase, when both set on a phase_committed row, make the
	// wait phase-exact (#11104): it parks on {scope_id, generation_id,
	// keyspace, phase}, its satisfaction reads that generation's own phase
	// row, and the phase Upsert wakes it by the exact key.
	Keyspace gpphase.Keyspace
	Phase    gpphase.Phase
	// ProviderLocality, required on every narrowed producer-terminal row,
	// says whether that producer commits in the wait's own scope and
	// generation or in other scopes; only cross-scope rows are reopened by
	// the cross-scope wake leg (#11114). Other rows leave it empty.
	ProviderLocality WaitingProviderLocality
}

// WaitingDeferralMap lists every non-counting readiness class plus the
// own-gate counting classes and the write-superseded classes. The
// completeness tests fail when any class in nonCountingReducerRetryFailureClasses
// or ownGateCountingClasses() lacks exactly one row, so a new class cannot
// be enrolled silently.
func WaitingDeferralMap() []WaitingDeferral {
	entries := append([]WaitingDeferral{}, waitingPhaseDeferrals()...)
	entries = append(entries, []WaitingDeferral{
		{
			FailureClass: crossscope.ProducerNotReadyFailureClass,
			WaitsForKind: WaitingKindProducerTerminal,
			WakeSite:     WaitingWakeSiteInTx,
			// #11000: one domain-only wait per producer domain, woken by
			// the producer reducer Ack and by the projector Ack that
			// activates the producer generation; the audit re-checks the
			// handler quiescence rule and the backstop still bounds a miss.
			ProvidingSave: "producer reducer Ack (AckBatch/Ack terminal commit) + projector-Ack generation activation (domain wakes by acked scope kind); audit quiescence fallback + backstop",
		},
		{
			FailureClass:  advisorygroup.WinnersWatermarkNotReadyFailureClass,
			WaitsForKind:  WaitingKindWinnersWatermark,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "winners maintainer resweep commit (ResweepWinners)",
		},
		{
			FailureClass:  reducer.CloudAdmissionNotReadyFailureClass,
			WaitsForKind:  WaitingKindFenceClear,
			WakeSite:      WaitingWakeSiteCompletion,
			ProvidingSave: "sibling Ack draining the admission fence; completion-event fanout",
		},
		{
			FailureClass:  crossscope.ValueFlowInputsNotReadyFailureClass,
			WaitsForKind:  WaitingKindFenceClear,
			WakeSite:      WaitingWakeSiteCompletion,
			ProvidingSave: "sink-chain writer terminal Ack; completion-event fanout",
		},
		{
			FailureClass:  workloadinstance.NotReadyFailureClass,
			WaitsForKind:  WaitingKindTargetCommitted,
			WakeSite:      WaitingWakeSiteAudit,
			ProvidingSave: "workload-instance endpoint commit; audit terminal-progress fallback + backstop",
		},
		{
			FailureClass:  iamcan.IAMCanPerformTargetNotReadyFailureClass,
			WaitsForKind:  WaitingKindTargetCommitted,
			WakeSite:      WaitingWakeSiteAudit,
			ProvidingSave: "sibling-scope target commit; audit terminal-progress fallback + backstop",
		},
		{
			// #11104: the handler gates on the generation's own
			// cross_repo_evidence/backward_evidence_committed row, which
			// only the deferred-backfill fan-in publishes, so the wait is
			// phase-exact and woken by that publish, not by sibling progress.
			FailureClass:  crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass,
			WaitsForKind:  WaitingKindPhaseCommitted,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "deferred-backfill fan-in publish of the generation's own backward_evidence_committed phase (in-tx exact wake + post-commit re-wake); audit exact-phase re-check + backstop",
			Keyspace:      gpphase.KeyspaceCrossRepoEvidence,
			Phase:         gpphase.PhaseBackwardEvidenceCommitted,
		},
		{
			FailureClass:  reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
			WaitsForKind:  WaitingKindRelationshipGenerationActive,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "relationship-generation activation transaction (activateWithConsumerWake, #7597)",
		},
		{
			FailureClass:  reducer.WorkloadMaterializationResolutionNotReadyFailureClass,
			WaitsForKind:  WaitingKindRelationshipGenerationActive,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "relationship-generation activation transaction (activateWithConsumerWake, #7597)",
		},
		{
			FailureClass:  servicecatalog.ServiceCatalogCorrelationResolutionNotReadyFailureClass,
			WaitsForKind:  WaitingKindRelationshipGenerationActive,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "relationship-generation activation transaction (activateWithConsumerWake, #7597)",
		},
		{
			FailureClass: reducercontract.GenerationActivationNotReadyFailureClass,
			WaitsForKind: WaitingKindGenerationActive,
			WakeSite:     WaitingWakeSiteInTx,
			// #11015: the wait resolves on terminal truth, not only on
			// activation: the projector Ack that activates the generation,
			// the terminal projector Fail that retires it, or the
			// projector Claim sweep that supersedes it. The audit
			// re-checks the same terminal predicate and the backstop
			// still bounds a miss.
			// #11228: the Ack re-runs its wakes after its commit, so a
			// waiter that parks inside the Ack's open window is reopened
			// without the audit (ack_post_commit wake site).
			// #11016: the Claim probe is scope-free (every scope the sweep
			// scanned, including empty sweeps) and post-commit wake
			// failures log and meter instead of failing the committed
			// operation; the heartbeat supersede and Ack's obsolete
			// maintenance wake in their own transactions. The retirement
			// fence, the generation-liveness sweep, and the delta-baseline
			// refusal retire generations off the hot path (leases drained
			// first), so they stay on the audit terminal-progress fallback
			// (at most two 5-minute audit cycles) plus the 10-minute
			// backstop; see the #11016 evidence note.
			ProvidingSave: "projector Ack activating the generation (in-tx wake + post-commit re-wake, #11228), terminal projector Fail, or projector Claim superseding the generation in any scope incl. empty sweeps (#11016); heartbeat supersede and Ack maintenance wakes in-tx; retirement fence, liveness sweep, and delta-baseline refusal via audit terminal-progress fallback (<=2 audit cycles) + 10-minute backstop",
		},
		{
			FailureClass:  reducer.WorkloadMaterializationDeploymentSourceTargetNotReadyFailureClass,
			WaitsForKind:  WaitingKindTargetCommitted,
			WakeSite:      WaitingWakeSiteAudit,
			ProvidingSave: "foreign-scope deployment-source commit; audit terminal-progress fallback + backstop",
		},
		{
			FailureClass:  reducer.SharedEdgeTargetNotReadyFailureClass,
			WaitsForKind:  WaitingKindTargetCommitted,
			WakeSite:      WaitingWakeSiteAudit,
			ProvidingSave: "sibling-scope endpoint commit; audit terminal-progress fallback + backstop",
		},
		{
			FailureClass: servicecatalog.RuntimeNotReadyFailureClass,
			WaitsForKind: WaitingKindProducerTerminal,
			WakeSite:     WaitingWakeSiteInTx,
			// #7816/#11114: the fence reads the correlated repository
			// scopes' workload rows while the wait is keyed to the catalog
			// scope, so only the cross-scope leg can reopen it.
			ProvidingSave:    "workload_materialization terminal Ack in any correlated repository scope (cross-scope leg)",
			ProducerDomain:   string(reducer.DomainWorkloadMaterialization),
			ProviderLocality: WaitingProviderCrossScope,
		},
		{
			FailureClass:     securityalert.ImpactNotReadyFailureClass,
			WaitsForKind:     WaitingKindProducerTerminal,
			WakeSite:         WaitingWakeSiteInTx,
			ProvidingSave:    "supply_chain_impact terminal Ack in the wait's scope and generation",
			ProducerDomain:   string(reducer.DomainSupplyChainImpact),
			ProviderLocality: WaitingProviderSameScopeGeneration,
		},
		{
			FailureClass:     supplychaincore.SBOMAttachmentNotReadyFailureClass,
			WaitsForKind:     WaitingKindProducerTerminal,
			WakeSite:         WaitingWakeSiteInTx,
			ProvidingSave:    "sbom_attestation_attachment terminal Ack in the wait's scope and generation (#11105)",
			ProducerDomain:   string(reducercontract.DomainSBOMAttestationAttachment),
			ProviderLocality: WaitingProviderSameScopeGeneration,
		},
		{
			FailureClass:  awscloud.AWSCloudRuntimeDriftWriteSupersededFailureClass,
			WaitsForKind:  WaitingKindTargetCommitted,
			WakeSite:      WaitingWakeSiteNotAWait,
			ProvidingSave: "none: a fresher admission already won; never parked",
		},
		{
			FailureClass:  awscloud.AWSCloudRuntimeDriftStatePendingFailureClass,
			WaitsForKind:  WaitingKindTargetCommitted,
			WakeSite:      WaitingWakeSiteNotAWait,
			ProvidingSave: "none: waits on a fresher snapshot, not a providing save; never parked",
		},
		{
			FailureClass:  supplychaincore.SupplyChainImpactWriteSupersededFailureClass,
			WaitsForKind:  WaitingKindTargetCommitted,
			WakeSite:      WaitingWakeSiteNotAWait,
			ProvidingSave: "none: a fresher pass already won; never parked",
		},
	}...)
	for _, class := range ownGateCountingClasses() {
		entries = append(entries, WaitingDeferral{
			FailureClass:  class,
			WaitsForKind:  WaitingKindRelationshipGenerationActive,
			WakeSite:      WaitingWakeSiteCounting,
			ProvidingSave: "relationship-generation activation (existing #7597 wake); counting, never parked",
		})
	}
	return entries
}

// WaitingDeferralByClass indexes WaitingDeferralMap by failure class. The
// index is built once: the Fail path consults it on every deferred intent
// and the audit on every parked row, so rebuilding the table per call would
// be pure allocation churn on a hot path.
func WaitingDeferralByClass() map[string]WaitingDeferral {
	waitingDeferralByClassOnce.Do(func() {
		byClass := make(map[string]WaitingDeferral)
		for _, entry := range WaitingDeferralMap() {
			byClass[entry.FailureClass] = entry
		}
		cachedWaitingDeferralByClass = byClass
	})
	return cachedWaitingDeferralByClass
}

var (
	waitingDeferralByClassOnce   sync.Once
	cachedWaitingDeferralByClass map[string]WaitingDeferral
)
