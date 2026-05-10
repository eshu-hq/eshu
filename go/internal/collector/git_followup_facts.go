package collector

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func workloadIdentityFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "workload_identity",
		"entity_key":     "workload:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted shared workload identity follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":workload_identity",
		payload,
		repoPath,
	)
}

func deployableUnitCorrelationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "deployable_unit_correlation",
		"entity_key":     "repo:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted deployable-unit correlation follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":deployable_unit_correlation",
		payload,
		repoPath,
	)
}

func codeCallMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "code_call_materialization",
		"entity_key":     "repo:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted code-call materialization follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":code_call_materialization",
		payload,
		repoPath,
	)
}

func workloadMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "workload_materialization",
		"entity_key":     "workload:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted workload materialization follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":workload_materialization",
		payload,
		repoPath,
	)
}

func deploymentMappingFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "deployment_mapping",
		"entity_key":     "deployment:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted deployment mapping follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":deployment_mapping",
		payload,
		repoPath,
	)
}

func sqlRelationshipMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "sql_relationship_materialization",
		"entity_key":     "sql:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted SQL relationship materialization follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":sql_relationship_materialization",
		payload,
		repoPath,
	)
}

func inheritanceMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "inheritance_materialization",
		"entity_key":     "inheritance:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted inheritance materialization follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":inheritance_materialization",
		payload,
		repoPath,
	)
}

func factEnvelope(
	factKind string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	factKey string,
	payload map[string]any,
	sourceURI string,
) facts.Envelope {
	return facts.Envelope{
		FactID: facts.StableID(
			"GoGitCollectorFact",
			map[string]any{
				"fact_key":      factKey,
				"fact_kind":     factKind,
				"generation_id": generationID,
				"scope_id":      scopeID,
			},
		),
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    factKey,
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        factKey,
			SourceURI:      sourceURI,
			SourceRecordID: factKey,
		},
	}
}

func repositoryRelativePath(repoPath string, filePath string) string {
	relativePath, err := filepath.Rel(repoPath, filePath)
	if err != nil {
		return filepath.Base(filePath)
	}
	return filepath.ToSlash(relativePath)
}

func payloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		return text
	}
	return ""
}

func payloadPath(payload map[string]any, key string) string {
	value := payloadString(payload, key)
	if value == "" {
		return ""
	}
	resolved, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return resolved
}
