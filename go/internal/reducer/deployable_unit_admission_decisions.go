// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/reducer/admissiondecision"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/accepted"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/relationships"
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
	now := admissiondecision.AdmissionNow(h.AdmissionDecisionNow)
	writes := make([]admissiondecision.AdmissionDecisionWrite, 0, len(evaluation.Results))
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
	return admissiondecision.WriteAdmissionDecisions(ctx, h.AdmissionDecisionWriter, writes)
}

func deployableUnitAdmissionDecision(
	intent Intent,
	candidate correlationmodel.Candidate,
	repoID string,
	deploymentRepoID string,
	canonicalWrites int,
	now time.Time,
) admissiondecision.AdmissionDecisionWrite {
	state := admissiondecision.AdmissionStateRejected
	canonical := admissiondecision.AdmissionCanonicalWrite{
		Eligible:      false,
		Written:       false,
		TargetKind:    DomainDeployableUnitEdges,
		SkippedReason: "candidate was not admitted",
	}
	if candidate.State == correlationmodel.CandidateStateAdmitted {
		if strings.TrimSpace(deploymentRepoID) == "" {
			state = admissiondecision.AdmissionStateMissingEvidence
			canonical.SkippedReason = "deployment repository evidence missing"
		} else {
			state = admissiondecision.AdmissionStateAdmitted
			canonical.Eligible = true
			canonical.Written = canonicalWrites > 0
			canonical.TargetID = admissiondecision.StableAdmissionDecisionID(
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
	candidateID := admissiondecision.StableAdmissionDecisionID(
		string(DomainDeployableUnitCorrelation),
		intent.GenerationID,
		candidate.CorrelationKey,
		deploymentRepoID,
	)
	decision := admissiondecision.NewAdmissionDecision(
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
	decision.ConfidenceBucket = admissiondecision.AdmissionConfidenceBucket(candidate.Confidence)
	decision.ConfidenceBasis = deployableUnitRulePackName(candidate)
	decision.SourceHandles = deployableUnitAdmissionSourceHandles(candidate, intent.ScopeID)
	decision.CanonicalWrite = canonical
	decision.RecommendedAction = deployableUnitAdmissionNextAction(state, candidate)

	evidence := make([]admissiondecision.AdmissionDecisionEvidence, 0, len(candidate.Evidence))
	for _, atom := range candidate.Evidence {
		sourceHandle := admissiondecision.StableAdmissionDecisionID(
			decision.DecisionID,
			atom.ID,
			atom.EvidenceType,
		)
		evidence = append(evidence, admissiondecision.NewAdmissionDecisionEvidence(
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
	return admissiondecision.AdmissionDecisionWrite{Decision: decision, Evidence: evidence}
}

func deployableUnitAdmissionSourceHandles(
	candidate correlationmodel.Candidate,
	scopeID string,
) []admissiondecision.AdmissionDecisionSourceHandle {
	handles := make([]admissiondecision.AdmissionDecisionSourceHandle, 0, len(candidate.Evidence))
	seen := make(map[string]struct{})
	for _, atom := range candidate.Evidence {
		id := admissiondecision.StableAdmissionDecisionID(atom.ID, atom.EvidenceType, atom.Key)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		handles = append(handles, admissiondecision.AdmissionDecisionSourceHandle{
			Kind:    atom.EvidenceType,
			ID:      id,
			ScopeID: payloadcore.FirstNonBlank(atom.ScopeID, scopeID),
		})
	}
	return handles
}

func deployableUnitAdmissionNextAction(
	state admissiondecision.AdmissionState,
	candidate correlationmodel.Candidate,
) admissiondecision.AdmissionNextAction {
	switch state {
	case admissiondecision.AdmissionStateAdmitted:
		return admissiondecision.AdmissionNextAction{Action: "none"}
	case admissiondecision.AdmissionStateMissingEvidence:
		return admissiondecision.AdmissionNextAction{
			Action: "add_deployment_repository_evidence",
			Reason: "deployable unit candidate was admitted but has no deployment repository target",
		}
	default:
		return admissiondecision.AdmissionNextAction{
			Action: "inspect_deployable_unit_evidence",
			Reason: deployableUnitDecisionReason(candidate),
		}
	}
}

// InputFingerprintedResolvedRelationshipLoader is the fused fenced by-repos
// read of CorpusFencedResolvedRelationshipLoader with the input fingerprint of
// the item's reads added in the SAME statement snapshot (#8283). ownGenerationID
// is the work item's own relationship generation. The fingerprint is an opaque
// string computed by SQL; callers store and never interpret it. When complete
// is false the rows and fingerprint are empty and must not be consumed.
type InputFingerprintedResolvedRelationshipLoader interface {
	GetResolvedRelationshipsForReposWithCorpusFenceAndInputFingerprint(
		ctx context.Context,
		ownGenerationID string,
		repoIDs []string,
	) (rows []relationships.ResolvedRelationship, complete bool, inputFingerprint string, err error)
}

// DeployableUnitInputFingerprintWriter records, per work item, the input
// fingerprint of the read a successful deployable_unit_correlation run used and
// the repository ids it covered. The deferred-maintenance reopen skips a
// succeeded item whose recorded fingerprint still matches the live one (#8283).
// RecordDeployableUnitInputFingerprint is an idempotent upsert keyed on the work
// item id.
type DeployableUnitInputFingerprintWriter interface {
	RecordDeployableUnitInputFingerprint(
		ctx context.Context,
		workItemID string,
		repoIDs []string,
		fingerprint string,
	) error
}

// readCorpusFencedResolvedRelationshipsWithFingerprint is
// readCorpusFencedResolvedRelationships for the deployable-unit handler: when
// the loader offers the fingerprinted fused read and the intent has
// candidates, the corpus fence, the foreign rows, and the input fingerprint
// come from one statement. Every other shape (a loader without the method, no
// candidates) takes the plain path and returns no fingerprint, so nothing is
// recorded and the next maintenance pass reopens the item (fail open).
func readCorpusFencedResolvedRelationshipsWithFingerprint(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	completeLookup accepted.RelationshipGenerationsCompleteLookup,
	intent Intent,
	candidates []WorkloadCandidate,
) (corpusFencedResolvedRead, error) {
	fenced, ok := loader.(InputFingerprintedResolvedRelationshipLoader)
	if !ok || len(candidates) == 0 {
		return readCorpusFencedResolvedRelationships(ctx, loader, completeLookup, intent, candidates)
	}
	repoResolved, complete, fingerprint, err := fenced.GetResolvedRelationshipsForReposWithCorpusFenceAndInputFingerprint(
		ctx, intent.GenerationID, workloadCandidateRepoIDs(candidates),
	)
	if err != nil {
		return corpusFencedResolvedRead{}, err
	}
	return finishFusedRead(ctx, loader, intent, repoResolved, complete, fingerprint)
}

// finishFusedRead completes a fused fenced read: an incomplete verdict returns
// before the own-scope read; otherwise the own-scope rows are merged with the
// foreign rows. The own-scope read follows the verdict so a deferral never pays
// for it (#6740 review F1). Rows added to the own generation between the two
// statements are covered by the input fingerprint's own row count (#8283),
// which was taken at the verdict's snapshot and can only be older than what
// this read sees.
func finishFusedRead(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	intent Intent,
	repoResolved []relationships.ResolvedRelationship,
	complete bool,
	inputFingerprint string,
) (corpusFencedResolvedRead, error) {
	if !complete {
		return corpusFencedResolvedRead{}, nil
	}
	resolved, err := loadResolvedRelationshipsForIntent(ctx, loader, intent)
	if err != nil {
		return corpusFencedResolvedRead{}, err
	}
	return corpusFencedResolvedRead{
		resolved:         mergeResolvedRelationships(resolved, repoResolved),
		complete:         true,
		inputFingerprint: inputFingerprint,
	}, nil
}

// recordInputFingerprint stores the fingerprint of a successful run, after the
// graph writes it vouches for and before the work item is acknowledged. It is a
// no-op without a writer or a fingerprint. A write failure fails the attempt:
// the queue retries, the graph writes are idempotent, and a missing or stale
// row can only cause an extra reopen, never a skipped one.
func (h DeployableUnitCorrelationHandler) recordInputFingerprint(
	ctx context.Context,
	intent Intent,
	candidates []WorkloadCandidate,
	read corpusFencedResolvedRead,
) error {
	if h.InputFingerprintWriter == nil || read.inputFingerprint == "" {
		return nil
	}
	if err := h.InputFingerprintWriter.RecordDeployableUnitInputFingerprint(
		ctx, intent.IntentID, workloadCandidateRepoIDs(candidates), read.inputFingerprint,
	); err != nil {
		return fmt.Errorf("record deployable unit input fingerprint: %w", err)
	}
	return nil
}
