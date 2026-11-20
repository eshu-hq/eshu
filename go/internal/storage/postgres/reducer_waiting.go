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
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2blockkms"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2instance"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2usesprofile"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamescalation"
	"github.com/eshu-hq/eshu/go/internal/reducer/iaminstprofile"
	"github.com/eshu-hq/eshu/go/internal/reducer/internetexposure"
	"github.com/eshu-hq/eshu/go/internal/reducer/kubernetescorrelation"
	"github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"
	"github.com/eshu-hq/eshu/go/internal/reducer/rdsposture"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3grant"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3logsto"
	"github.com/eshu-hq/eshu/go/internal/reducer/secgroup"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
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
	// WaitingKindGenerationActive waits for the projector Ack that activates
	// the generation. Key: {scope_id, generation_id}.
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
	// producer domain's terminal Ack in the wait's scope and generation.
	ProducerDomain string
}

// waitingPhaseDeferral builds one phase-commit map row: the waiter's
// canonical nodes commit with the producer's phase publish.
func waitingPhaseDeferral(class, save string) WaitingDeferral {
	return WaitingDeferral{
		FailureClass:  class,
		WaitsForKind:  WaitingKindPhaseCommitted,
		WakeSite:      WaitingWakeSiteInTx,
		ProvidingSave: save,
	}
}

// waitingPhaseDeferrals lists the phase-commit family: materialization
// domains whose readiness gate waits on the producer's canonical-node phase
// commit (the largest deferral family in the #10876 census).
func waitingPhaseDeferrals() []WaitingDeferral {
	phase := waitingPhaseDeferral
	save := "producer phase-commit upsert (graph_projection_phase_state)"
	return []WaitingDeferral{
		phase(secretsiam.SecretsIAMEndpointNotReadyFailureClass, save),
		phase(kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass, save),
		phase(reducer.GCPRelationshipNodesNotReadyFailureClass, save),
		phase(ec2instance.EC2InstanceIdentityNodesNotReadyFailureClass, save),
		phase(reducer.AWSRelationshipEC2InstanceNodesNotReadyFailureClass, save),
		phase(awscloud.AWSCloudImageNodesNotReadyFailureClass, save),
		phase(reducer.AWSRelationshipNodesNotReadyFailureClass, save),
		phase(reducer.AzureRelationshipNodesNotReadyFailureClass, save),
		phase(iamcan.IAMCanAssumeNodesNotReadyFailureClass, save),
		phase(iamcan.IAMCanPerformNodesNotReadyFailureClass, save),
		phase(iamescalation.IAMEscalationNodesNotReadyFailureClass, save),
		phase(iaminstprofile.IAMInstanceProfileRoleNodesNotReadyFailureClass, save),
		phase(obscoverage.ObservabilityCoverageNodesNotReadyFailureClass, save),
		phase(rdsposture.RDSPostureNodesNotReadyFailureClass, save),
		phase(secgroup.SecurityGroupReachabilityNodesNotReadyFailureClass, save),
		phase(reducer.WorkloadCloudRelationshipNodesNotReadyFailureClass, save),
		phase(ec2blockkms.EC2BlockDeviceKMSPostureNodesNotReadyFailureClass, save),
		phase(internetexposure.EC2InternetExposureNodesNotReadyFailureClass, save),
		phase(ec2usesprofile.EC2UsesProfileNodesNotReadyFailureClass, save),
		phase(s3grant.S3ExternalPrincipalGrantNodesNotReadyFailureClass, save),
		phase(internetexposure.S3InternetExposureNodesNotReadyFailureClass, save),
		phase(s3logsto.S3LogsToNodesNotReadyFailureClass, save),
		{
			FailureClass:  reducer.DeployableUnitCorrelationCanonicalNodesNotReadyFailureClass,
			WaitsForKind:  WaitingKindPhaseCommitted,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "producer phase-commit upsert (graph_projection_phase_state)",
		},
	}
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
			FailureClass:  crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass,
			WaitsForKind:  WaitingKindTargetCommitted,
			WakeSite:      WaitingWakeSiteAudit,
			ProvidingSave: "foreign-scope evidence commit; audit terminal-progress fallback + backstop",
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
			FailureClass:  reducercontract.GenerationActivationNotReadyFailureClass,
			WaitsForKind:  WaitingKindGenerationActive,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "projector Ack activating the generation",
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
			FailureClass:   servicecatalog.RuntimeNotReadyFailureClass,
			WaitsForKind:   WaitingKindProducerTerminal,
			WakeSite:       WaitingWakeSiteInTx,
			ProvidingSave:  "workload_materialization terminal Ack in the wait's scope and generation",
			ProducerDomain: string(reducer.DomainWorkloadMaterialization),
		},
		{
			FailureClass:   securityalert.ImpactNotReadyFailureClass,
			WaitsForKind:   WaitingKindProducerTerminal,
			WakeSite:       WaitingWakeSiteInTx,
			ProvidingSave:  "supply_chain_impact terminal Ack in the wait's scope and generation",
			ProducerDomain: string(reducer.DomainSupplyChainImpact),
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
