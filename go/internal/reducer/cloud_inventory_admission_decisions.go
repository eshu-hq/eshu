// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
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
	now := admissionNow(h.AdmissionDecisionNow)
	written := make(map[string]struct{}, len(writeResult.CanonicalIDs))
	for _, id := range writeResult.CanonicalIDs {
		written[strings.TrimSpace(id)] = struct{}{}
	}
	writes := make([]AdmissionDecisionWrite, 0, len(records))
	for _, record := range records {
		writes = append(writes, cloudInventoryAdmissionDecision(intent, record, written, now))
	}
	return writeAdmissionDecisions(ctx, h.AdmissionDecisionWriter, writes)
}

func cloudInventoryAdmissionDecision(
	intent Intent,
	record CloudInventoryRecord,
	written map[string]struct{},
	now time.Time,
) AdmissionDecisionWrite {
	resolution := cloudinventory.ResolveProviderIdentity(record.Provider, record.RawIdentity)
	state := cloudInventoryAdmissionState(resolution.Outcome)
	candidateID := cloudInventoryCandidateID(intent, record, resolution)
	canonical := AdmissionCanonicalWrite{
		Eligible:      false,
		Written:       false,
		TargetKind:    cloudInventoryAdmissionFactKind,
		SkippedReason: cloudInventoryAdmissionSkippedReason(state),
	}
	if state == AdmissionStateAdmitted {
		canonical.Eligible = true
		canonical.TargetID = resolution.CloudResourceUID
		_, canonical.Written = written[resolution.CloudResourceUID]
		if canonical.Written {
			canonical.SkippedReason = ""
		}
	}

	handleID := cloudInventorySourceHandleID(intent, record)
	decision := newAdmissionDecision(
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
	decision.ConfidenceBucket = admissionConfidenceBucket(decision.ConfidenceScore)
	decision.ConfidenceBasis = string(record.SourceLayer)
	decision.SourceHandles = []AdmissionDecisionSourceHandle{{
		Kind:    record.FactKind,
		ID:      handleID,
		ScopeID: intent.ScopeID,
	}}
	decision.CanonicalWrite = canonical
	decision.RecommendedAction = cloudInventoryAdmissionNextAction(state)

	return AdmissionDecisionWrite{
		Decision: decision,
		Evidence: []AdmissionDecisionEvidence{
			admissionDecisionEvidence(
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

func cloudInventoryAdmissionState(outcome cloudinventory.ResolutionOutcome) AdmissionState {
	switch outcome {
	case cloudinventory.ResolutionOutcomeAdmitted:
		return AdmissionStateAdmitted
	case cloudinventory.ResolutionOutcomeAmbiguous:
		return AdmissionStateAmbiguous
	case cloudinventory.ResolutionOutcomeUnsupported:
		return AdmissionStateUnsupported
	default:
		return AdmissionStateMissingEvidence
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
	return stableAdmissionDecisionID(
		string(DomainCloudInventoryAdmission),
		intent.ScopeID,
		intent.GenerationID,
		record.Provider,
		record.FactKind,
		record.RawIdentity,
	)
}

func cloudInventorySourceHandleID(intent Intent, record CloudInventoryRecord) string {
	return stableAdmissionDecisionID(
		string(DomainCloudInventoryAdmission),
		intent.ScopeID,
		intent.GenerationID,
		record.Provider,
		record.FactKind,
		record.RawIdentity,
	)
}

func cloudInventoryDecisionConfidence(state AdmissionState) float64 {
	if state == AdmissionStateAdmitted {
		return 1
	}
	return 0
}

func cloudInventoryAdmissionSkippedReason(state AdmissionState) string {
	switch state {
	case AdmissionStateAmbiguous:
		return "provider identity is ambiguous"
	case AdmissionStateUnsupported:
		return "provider identity is unsupported"
	case AdmissionStateMissingEvidence:
		return "provider identity evidence is missing"
	default:
		return "candidate was not admitted"
	}
}

func cloudInventoryAdmissionNextAction(state AdmissionState) AdmissionNextAction {
	switch state {
	case AdmissionStateAdmitted:
		return AdmissionNextAction{Action: "none"}
	case AdmissionStateAmbiguous:
		return AdmissionNextAction{Action: "normalize_provider_identity"}
	case AdmissionStateUnsupported:
		return AdmissionNextAction{Action: "add_provider_support"}
	default:
		return AdmissionNextAction{Action: "add_provider_identity"}
	}
}
