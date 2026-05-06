package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// RelationshipStageResult captures the output of the relationship projection
// stage.
type RelationshipStageResult struct {
	Intents []ReducerIntent
}

// ProjectRelationshipStage projects relationship-bearing facts into reducer
// intents. Relationship facts are any facts that carry a reducer_domain
// payload key.
func ProjectRelationshipStage(envelopes []facts.Envelope) RelationshipStageResult {
	result := RelationshipStageResult{}

	seen := make(map[string]struct{}, len(envelopes))
	for i := range envelopes {
		if _, ok := seen[envelopes[i].FactID]; ok {
			continue
		}
		seen[envelopes[i].FactID] = struct{}{}

		if intent, ok := buildReducerIntent(envelopes[i]); ok {
			result.Intents = append(result.Intents, intent)
		}
	}

	return result
}
