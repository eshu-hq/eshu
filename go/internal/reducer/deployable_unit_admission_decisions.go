// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
)

func (h DeployableUnitCorrelationHandler) writeDeployableUnitAdmissionDecisions(
	ctx context.Context,
	intent Intent,
	evaluation engine.Evaluation,
	canonicalWrites int,
) error {
	if h.AdmissionDecisionWriter == nil {
		return nil
	}
	now := admissionNow(h.AdmissionDecisionNow)
	writes := make([]AdmissionDecisionWrite, 0, len(evaluation.Results))
	for _, result := range evaluation.Results {
		candidate := result.Candidate
		repoID := deployableUnitEvidenceValue(candidate, "repo_id")
		if repoID == "" {
			continue
		}
		deploymentRepoIDs := deployableUnitEvidenceValues(candidate, "deployment_repo_id")
		if len(deploymentRepoIDs) == 0 {
			deploymentRepoIDs = []string{""}
		}
		for _, deploymentRepoID := range deploymentRepoIDs {
			writes = append(writes, deployableUnitAdmissionDecision(
				intent,
				candidate,
				repoID,
				deploymentRepoID,
				canonicalWrites,
				now,
			))
		}
	}
	return writeAdmissionDecisions(ctx, h.AdmissionDecisionWriter, writes)
}

func deployableUnitAdmissionDecision(
	intent Intent,
	candidate correlationmodel.Candidate,
	repoID string,
	deploymentRepoID string,
	canonicalWrites int,
	now time.Time,
) AdmissionDecisionWrite {
	state := AdmissionStateRejected
	canonical := AdmissionCanonicalWrite{
		Eligible:      false,
		Written:       false,
		TargetKind:    DomainDeployableUnitEdges,
		SkippedReason: "candidate was not admitted",
	}
	if candidate.State == correlationmodel.CandidateStateAdmitted {
		if strings.TrimSpace(deploymentRepoID) == "" {
			state = AdmissionStateMissingEvidence
			canonical.SkippedReason = "deployment repository evidence missing"
		} else {
			state = AdmissionStateAdmitted
			canonical.Eligible = true
			canonical.Written = canonicalWrites > 0
			canonical.TargetID = stableAdmissionDecisionID(
				string(DomainDeployableUnitCorrelation),
				intent.GenerationID,
				repoID,
				deploymentRepoID,
				candidate.CorrelationKey,
			)
			if !canonical.Written {
				canonical.SkippedReason = "deployable unit edge writer unavailable"
			} else {
				canonical.SkippedReason = ""
			}
		}
	}
	anchorID := strings.TrimSpace(repoID)
	candidateID := stableAdmissionDecisionID(
		string(DomainDeployableUnitCorrelation),
		intent.GenerationID,
		candidate.CorrelationKey,
		deploymentRepoID,
	)
	decision := newAdmissionDecision(
		DomainDeployableUnitCorrelation,
		state,
		string(candidate.State),
		intent.ScopeID,
		intent.GenerationID,
		"repository",
		anchorID,
		"deployable_unit",
		candidateID,
		now,
	)
	decision.ConfidenceScore = candidate.Confidence
	decision.ConfidenceBucket = admissionConfidenceBucket(candidate.Confidence)
	decision.ConfidenceBasis = deployableUnitRulePackName(candidate)
	decision.SourceHandles = deployableUnitAdmissionSourceHandles(candidate, intent.ScopeID)
	decision.CanonicalWrite = canonical
	decision.RecommendedAction = deployableUnitAdmissionNextAction(state, candidate)

	evidence := make([]AdmissionDecisionEvidence, 0, len(candidate.Evidence))
	for _, atom := range candidate.Evidence {
		sourceHandle := stableAdmissionDecisionID(
			decision.DecisionID,
			atom.ID,
			atom.EvidenceType,
		)
		evidence = append(evidence, admissionDecisionEvidence(
			decision,
			sourceHandle,
			atom.EvidenceType,
			map[string]any{
				"key":        atom.Key,
				"scope_id":   atom.ScopeID,
				"confidence": atom.Confidence,
			},
			now,
		))
	}
	return AdmissionDecisionWrite{Decision: decision, Evidence: evidence}
}

func deployableUnitAdmissionSourceHandles(
	candidate correlationmodel.Candidate,
	scopeID string,
) []AdmissionDecisionSourceHandle {
	handles := make([]AdmissionDecisionSourceHandle, 0, len(candidate.Evidence))
	seen := make(map[string]struct{})
	for _, atom := range candidate.Evidence {
		id := stableAdmissionDecisionID(atom.ID, atom.EvidenceType, atom.Key)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		handles = append(handles, AdmissionDecisionSourceHandle{
			Kind:    atom.EvidenceType,
			ID:      id,
			ScopeID: firstNonBlank(atom.ScopeID, scopeID),
		})
	}
	return handles
}

func deployableUnitAdmissionNextAction(
	state AdmissionState,
	candidate correlationmodel.Candidate,
) AdmissionNextAction {
	switch state {
	case AdmissionStateAdmitted:
		return AdmissionNextAction{Action: "none"}
	case AdmissionStateMissingEvidence:
		return AdmissionNextAction{
			Action: "add_deployment_repository_evidence",
			Reason: "deployable unit candidate was admitted but has no deployment repository target",
		}
	default:
		return AdmissionNextAction{
			Action: "inspect_deployable_unit_evidence",
			Reason: deployableUnitDecisionReason(candidate),
		}
	}
}
