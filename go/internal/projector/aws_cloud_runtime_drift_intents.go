package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildAWSCloudRuntimeDriftReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainAWSCloudRuntimeDrift,
			EntityKey:    "aws_cloud_runtime_drift:" + scopeValue.ScopeID,
			Reason:       "aws runtime resource facts observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}

func awsCloudRuntimeDriftSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
