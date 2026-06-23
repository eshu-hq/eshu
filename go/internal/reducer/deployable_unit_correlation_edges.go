package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

const (
	deployableUnitCorrelationEvidenceSource   = "reducer/deployable-unit-correlation"
	deployableUnitCorrelationRelationshipType = string(edgetype.CorrelatesDeployableUnit)
)

func (h DeployableUnitCorrelationHandler) materializeDeployableUnitEdges(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
) (int, error) {
	if h.EdgeWriter == nil || len(rows) == 0 {
		return 0, nil
	}
	if err := h.retractDeployableUnitEdges(ctx, rows); err != nil {
		return 0, err
	}
	writeRows := admittedDeployableUnitRows(rows)
	if len(writeRows) == 0 {
		return 0, nil
	}
	if err := h.EdgeWriter.WriteEdges(ctx, DomainDeployableUnitEdges, writeRows, deployableUnitCorrelationEvidenceSource); err != nil {
		return 0, fmt.Errorf("write deployable unit correlation edges: %w", err)
	}
	return len(writeRows), nil
}

func (h DeployableUnitCorrelationHandler) retractDeployableUnitEdges(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
) error {
	if h.EdgeWriter == nil || len(rows) == 0 {
		return nil
	}
	if err := h.EdgeWriter.RetractEdges(ctx, DomainDeployableUnitEdges, rows, deployableUnitCorrelationEvidenceSource); err != nil {
		return fmt.Errorf("retract deployable unit correlation edges: %w", err)
	}
	return nil
}

func admittedDeployableUnitRows(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	admitted := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(anyToString(row.Payload["admission_state"])) != string(correlationmodel.CandidateStateAdmitted) {
			continue
		}
		if strings.TrimSpace(anyToString(row.Payload["deployment_repo_id"])) == "" {
			continue
		}
		admitted = append(admitted, row)
	}
	return admitted
}

func deployableUnitCorrelationRows(
	intent Intent,
	evaluation engine.Evaluation,
) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(evaluation.Results))
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
			rows = append(rows, deployableUnitCorrelationRow(intent, candidate, repoID, deploymentRepoID))
		}
	}
	return rows
}

func deployableUnitRetractRowsFromFacts(intent Intent, envelopes []facts.Envelope) []SharedProjectionIntentRow {
	var rows []SharedProjectionIntentRow
	for _, envelope := range envelopes {
		if envelope.FactKind != factKindRepository {
			continue
		}
		repoID := strings.TrimSpace(anyToString(envelope.Payload["graph_id"]))
		if repoID == "" {
			repoID = strings.TrimSpace(anyToString(envelope.Payload["repo_id"]))
		}
		if repoID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			IntentID:         intent.IntentID,
			ProjectionDomain: DomainDeployableUnitEdges,
			PartitionKey:     repoID,
			ScopeID:          intent.ScopeID,
			AcceptanceUnitID: deployableUnitAcceptanceUnitID(intent),
			RepositoryID:     repoID,
			SourceRunID:      intent.GenerationID,
			GenerationID:     intent.GenerationID,
			Payload: map[string]any{
				"repo_id":            repoID,
				"scope_id":           intent.ScopeID,
				"acceptance_unit_id": deployableUnitAcceptanceUnitID(intent),
				"generation_id":      intent.GenerationID,
			},
		})
	}
	return rows
}

func deployableUnitCorrelationRow(
	intent Intent,
	candidate correlationmodel.Candidate,
	repoID string,
	deploymentRepoID string,
) SharedProjectionIntentRow {
	unitKey := deployableUnitEvidenceValue(candidate, "deployable_unit_key")
	acceptanceUnitID := deployableUnitAcceptanceUnitID(intent)
	return SharedProjectionIntentRow{
		IntentID:         intent.IntentID,
		ProjectionDomain: DomainDeployableUnitEdges,
		PartitionKey:     repoID,
		ScopeID:          intent.ScopeID,
		AcceptanceUnitID: acceptanceUnitID,
		RepositoryID:     repoID,
		SourceRunID:      intent.GenerationID,
		GenerationID:     intent.GenerationID,
		CreatedAt:        time.Now().UTC(),
		Payload: map[string]any{
			"repo_id":             repoID,
			"deployment_repo_id":  deploymentRepoID,
			"deployable_unit_key": unitKey,
			"correlation_key":     candidate.CorrelationKey,
			"relationship_type":   deployableUnitCorrelationRelationshipType,
			"evidence_type":       "deployable_unit_correlation",
			"resolution_source":   deployableUnitCorrelationEvidenceSource,
			"generation_id":       intent.GenerationID,
			"source_system":       intent.SourceSystem,
			"scope_id":            intent.ScopeID,
			"acceptance_unit_id":  acceptanceUnitID,
			"admission_state":     string(candidate.State),
			"confidence":          candidate.Confidence,
			"evidence_count":      len(candidate.Evidence),
			"evidence_kinds":      deployableUnitEvidenceKinds(candidate),
			"rule_pack":           deployableUnitRulePackName(candidate),
			"reason":              deployableUnitDecisionReason(candidate),
			"resolved_id":         fmt.Sprintf("deployable-unit-correlation:%s:%s", intent.GenerationID, candidate.CorrelationKey),
		},
	}
}

func deployableUnitAcceptanceUnitID(intent Intent) string {
	entityKeys := uniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) > 0 {
		return normalizedEntityKey(entityKeys[0])
	}
	return strings.TrimSpace(intent.ScopeID)
}

func deployableUnitEvidenceValue(candidate correlationmodel.Candidate, key string) string {
	for _, evidence := range candidate.Evidence {
		if evidence.Key == key && strings.TrimSpace(evidence.Value) != "" {
			return strings.TrimSpace(evidence.Value)
		}
	}
	return ""
}

func deployableUnitEvidenceValues(candidate correlationmodel.Candidate, key string) []string {
	var values []string
	for _, evidence := range candidate.Evidence {
		if evidence.Key != key {
			continue
		}
		values = appendUniqueString(values, evidence.Value)
	}
	return values
}

func deployableUnitEvidenceKinds(candidate correlationmodel.Candidate) []string {
	kinds := make([]string, 0, len(candidate.Evidence))
	for _, evidence := range candidate.Evidence {
		kinds = append(kinds, evidence.EvidenceType)
	}
	return uniqueSortedStrings(kinds)
}

func deployableUnitRulePackName(candidate correlationmodel.Candidate) string {
	switch {
	case deployableUnitHasEvidence(candidate, "argocd"):
		return "argocd"
	case deployableUnitHasEvidence(candidate, "kustomize"):
		return "kustomize"
	case deployableUnitHasEvidence(candidate, "helm"):
		return "helm"
	case deployableUnitHasEvidence(candidate, "jenkins"):
		return "jenkins"
	case deployableUnitHasEvidence(candidate, "github_actions"):
		return "github_actions"
	case deployableUnitHasEvidence(candidate, "docker_compose"):
		return "docker_compose"
	case deployableUnitHasEvidence(candidate, "cloudformation"):
		return "cloudformation"
	case deployableUnitHasEvidence(candidate, "dockerfile"):
		return "dockerfile"
	default:
		return "deployable-unit-fallback"
	}
}

func deployableUnitHasEvidence(candidate correlationmodel.Candidate, evidenceType string) bool {
	for _, evidence := range candidate.Evidence {
		if evidence.EvidenceType == evidenceType || strings.HasPrefix(evidence.EvidenceType, evidenceType+":") {
			return true
		}
	}
	return false
}

func deployableUnitDecisionReason(candidate correlationmodel.Candidate) string {
	if candidate.State == correlationmodel.CandidateStateAdmitted {
		return "admitted deployable unit correlation"
	}
	reasons := make([]string, 0, len(candidate.RejectionReasons))
	for _, reason := range candidate.RejectionReasons {
		reasons = append(reasons, string(reason))
	}
	if len(reasons) == 0 {
		return "deployable unit correlation not admitted"
	}
	return "deployable unit correlation rejected: " + strings.Join(uniqueSortedStrings(reasons), ",")
}
