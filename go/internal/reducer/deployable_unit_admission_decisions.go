// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/reducer/admissiondecision"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// DeployableUnitEdgeWriteUnverifiedFailureClass classifies a
// deployable_unit_correlation run whose CORRELATES_DEPLOYABLE_UNIT edge write
// reported success but whose edge the post-write verification did not find,
// although both endpoint Repository nodes exist (#8283). That is not a timing
// state, so the class counts toward the retry budget and a repeat
// dead-letters loudly; a missing endpoint node instead returns the
// non-counting SharedEdgeTargetNotReadyFailureClass.
const DeployableUnitEdgeWriteUnverifiedFailureClass = "deployable_unit_edge_write_unverified"

// ErrDeployableUnitEdgeVerificationUnavailable is returned by a
// DeployableUnitEdgeVerifier whose graph executor cannot run read probes. The
// handler then treats the run as unverified: it still succeeds, but records
// no reopen fingerprint, so the next maintenance pass re-opens it (#8283).
var ErrDeployableUnitEdgeVerificationUnavailable = errors.New("deployable unit edge verification unavailable: graph executor has no probe capability")

// DeployableUnitEdgeVerifier proves that every admitted
// CORRELATES_DEPLOYABLE_UNIT edge row is present in the graph after the write
// (#8283, verified success). It returns nil when every edge is present, an
// error classed SharedEdgeTargetNotReadyFailureClass when an edge is missing
// because an endpoint Repository node is absent, an error classed
// DeployableUnitEdgeWriteUnverifiedFailureClass when the edge is missing with
// both endpoints present, and an unclassed retryable error when a probe could
// not run. The edge writer implements it.
type DeployableUnitEdgeVerifier interface {
	VerifyDeployableUnitEdges(ctx context.Context, rows []sharedintent.Row, evidenceSource string) error
}

// verifyDeployableUnitEdges proves the admitted edge rows the write just
// MERGEd are present in the graph (#8283, verified success). The canonical
// upsert MATCHes both endpoint Repository nodes, so a write whose endpoint did
// not exist yet reports success while writing nothing; recording the reopen
// fingerprint after such a run would keep the item closed with its edge
// missing. It reports verified=true only when there was nothing to write or
// every admitted edge was found. A nil EdgeVerifier, a verifier that reports
// ErrDeployableUnitEdgeVerificationUnavailable, or admitted rows with no
// EdgeWriter, reports verified=false with no error: the run still succeeds,
// but records no fingerprint, so the next maintenance pass re-opens it.
func (h DeployableUnitCorrelationHandler) verifyDeployableUnitEdges(
	ctx context.Context,
	rows []sharedintent.Row,
) (bool, error) {
	admitted := AdmittedDeployableUnitRows(rows)
	if len(admitted) == 0 {
		return true, nil
	}
	if h.EdgeWriter == nil || h.EdgeVerifier == nil {
		logDeployableUnitUnverifiedRun(h.Logger, "no edge verifier wired")
		return false, nil
	}
	if err := h.EdgeVerifier.VerifyDeployableUnitEdges(ctx, admitted, deployableUnitCorrelationEvidenceSource); err != nil {
		if errors.Is(err, ErrDeployableUnitEdgeVerificationUnavailable) {
			logDeployableUnitUnverifiedRun(h.Logger, "edge verification unavailable")
			return false, nil
		}
		return false, fmt.Errorf("verify deployable unit correlation edges: %w", err)
	}
	return true, nil
}

// logDeployableUnitUnverifiedRun records the fail-open unverified path at
// debug level so a fleet whose executor never verifies stays attributable
// (#8283): the run succeeds and records no reopen fingerprint, keeping the
// unconditional reopen. A nil logger keeps the pre-existing silent behavior.
func logDeployableUnitUnverifiedRun(logger *slog.Logger, reason string) {
	if logger == nil {
		return
	}
	logger.Debug("deployable unit correlation run unverified: recording no reopen fingerprint", "reason", reason)
}

func (h DeployableUnitCorrelationHandler) writeDeployableUnitAdmissionDecisions(
	ctx context.Context,
	intent reducercontract.Intent,
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
	intent reducercontract.Intent,
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
		TargetKind:    reducercontract.DomainDeployableUnitEdges,
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
