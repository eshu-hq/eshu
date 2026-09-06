// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/reducer/admissiondecision"
)

func (h CloudInventoryAdmissionHandler) writeCloudInventoryAdmissionDecisions(
	ctx context.Context,
	intent Intent,
	records []CloudInventoryRecord,
	writeResult CloudInventoryAdmissionWriteResult,
) error {
	if h.AdmissionDecisionWriter == nil {
		return nil
	}
	now := admissiondecision.AdmissionNow(h.AdmissionDecisionNow)
	written := make(map[string]struct{}, len(writeResult.CanonicalIDs))
	for _, id := range writeResult.CanonicalIDs {
		written[strings.TrimSpace(id)] = struct{}{}
	}
	writes := make([]admissiondecision.AdmissionDecisionWrite, 0, len(records))
	for _, record := range records {
		writes = append(writes, cloudInventoryAdmissionDecision(intent, record, written, now))
	}
	return admissiondecision.WriteAdmissionDecisions(ctx, h.AdmissionDecisionWriter, writes)
}

func cloudInventoryAdmissionDecision(
	intent Intent,
	record CloudInventoryRecord,
	written map[string]struct{},
	now time.Time,
) admissiondecision.AdmissionDecisionWrite {
	resolution := cloudinventory.ResolveProviderIdentity(record.Provider, record.RawIdentity)
	state := cloudInventoryAdmissionState(resolution.Outcome)
	candidateID := cloudInventoryCandidateID(intent, record, resolution)
	canonical := admissiondecision.AdmissionCanonicalWrite{
		Eligible:      false,
		Written:       false,
		TargetKind:    cloudInventoryAdmissionFactKind,
		SkippedReason: cloudInventoryAdmissionSkippedReason(state),
	}
	if state == admissiondecision.AdmissionStateAdmitted {
		canonical.Eligible = true
		canonical.TargetID = resolution.CloudResourceUID
		_, canonical.Written = written[resolution.CloudResourceUID]
		if canonical.Written {
			canonical.SkippedReason = ""
		}
	}

	handleID := cloudInventorySourceHandleID(intent, record)
	decision := admissiondecision.NewAdmissionDecision(
		DomainCloudInventoryAdmission,
		state,
		string(resolution.Outcome),
		intent.ScopeID,
		intent.GenerationID,
		"cloud_inventory_record",
		handleID,
		"cloud_resource",
		candidateID,
		now,
	)
	decision.ConfidenceScore = cloudInventoryDecisionConfidence(state)
	decision.ConfidenceBucket = admissiondecision.AdmissionConfidenceBucket(decision.ConfidenceScore)
	decision.ConfidenceBasis = string(record.SourceLayer)
	decision.SourceHandles = []admissiondecision.AdmissionDecisionSourceHandle{{
		Kind:    record.FactKind,
		ID:      handleID,
		ScopeID: intent.ScopeID,
	}}
	decision.CanonicalWrite = canonical
	decision.RecommendedAction = cloudInventoryAdmissionNextAction(state)

	return admissiondecision.AdmissionDecisionWrite{
		Decision: decision,
		Evidence: []admissiondecision.AdmissionDecisionEvidence{
			admissiondecision.NewAdmissionDecisionEvidence(
				decision,
				handleID,
				record.FactKind,
				map[string]any{
					"provider":      record.Provider,
					"fact_kind":     record.FactKind,
					"resource_type": record.ResourceType,
					"source_layer":  string(record.SourceLayer),
					"outcome":       string(resolution.Outcome),
				},
				now,
			),
		},
	}
}

func cloudInventoryAdmissionState(outcome cloudinventory.ResolutionOutcome) admissiondecision.AdmissionState {
	switch outcome {
	case cloudinventory.ResolutionOutcomeAdmitted:
		return admissiondecision.AdmissionStateAdmitted
	case cloudinventory.ResolutionOutcomeAmbiguous:
		return admissiondecision.AdmissionStateAmbiguous
	case cloudinventory.ResolutionOutcomeUnsupported:
		return admissiondecision.AdmissionStateUnsupported
	default:
		return admissiondecision.AdmissionStateMissingEvidence
	}
}

func cloudInventoryCandidateID(
	intent Intent,
	record CloudInventoryRecord,
	resolution cloudinventory.Resolution,
) string {
	if strings.TrimSpace(resolution.CloudResourceUID) != "" {
		return resolution.CloudResourceUID
	}
	return admissiondecision.StableAdmissionDecisionID(
		string(DomainCloudInventoryAdmission),
		intent.ScopeID,
		intent.GenerationID,
		record.Provider,
		record.FactKind,
		record.RawIdentity,
	)
}

func cloudInventorySourceHandleID(intent Intent, record CloudInventoryRecord) string {
	return admissiondecision.StableAdmissionDecisionID(
		string(DomainCloudInventoryAdmission),
		intent.ScopeID,
		intent.GenerationID,
		record.Provider,
		record.FactKind,
		record.RawIdentity,
	)
}

func cloudInventoryDecisionConfidence(state admissiondecision.AdmissionState) float64 {
	if state == admissiondecision.AdmissionStateAdmitted {
		return 1
	}
	return 0
}

func cloudInventoryAdmissionSkippedReason(state admissiondecision.AdmissionState) string {
	switch state {
	case admissiondecision.AdmissionStateAmbiguous:
		return "provider identity is ambiguous"
	case admissiondecision.AdmissionStateUnsupported:
		return "provider identity is unsupported"
	case admissiondecision.AdmissionStateMissingEvidence:
		return "provider identity evidence is missing"
	default:
		return "candidate was not admitted"
	}
}

func cloudInventoryAdmissionNextAction(state admissiondecision.AdmissionState) admissiondecision.AdmissionNextAction {
	switch state {
	case admissiondecision.AdmissionStateAdmitted:
		return admissiondecision.AdmissionNextAction{Action: "none"}
	case admissiondecision.AdmissionStateAmbiguous:
		return admissiondecision.AdmissionNextAction{Action: "normalize_provider_identity"}
	case admissiondecision.AdmissionStateUnsupported:
		return admissiondecision.AdmissionNextAction{Action: "add_provider_support"}
	default:
		return admissiondecision.AdmissionNextAction{Action: "add_provider_identity"}
	}
}
