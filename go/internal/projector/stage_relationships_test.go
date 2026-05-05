package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRelationshipStageBuildsReducerIntents(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:       "i-1",
			FactKind:     "file",
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			Payload: map[string]any{
				"reducer_domain": "data_lineage",
				"entity_key":     "repo:r1",
				"reason":         "import detected",
			},
		},
	}

	result := ProjectRelationshipStage(envelopes)
	if len(result.Intents) != 1 {
		t.Fatalf("Intents len = %d, want 1", len(result.Intents))
	}
	if result.Intents[0].EntityKey != "repo:r1" {
		t.Errorf("EntityKey = %q", result.Intents[0].EntityKey)
	}
}

func TestRelationshipStageDeduplicatesIntents(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			FactKind: "file",
			Payload: map[string]any{
				"reducer_domain": "data_lineage",
				"entity_key":     "repo:r1",
			},
		},
		{
			FactID:   "r-1",
			FactKind: "file",
			Payload: map[string]any{
				"reducer_domain": "data_lineage",
				"entity_key":     "repo:r1",
			},
		},
	}

	result := ProjectRelationshipStage(envelopes)
	if len(result.Intents) != 1 {
		t.Errorf("Intents len = %d, want 1 (deduped)", len(result.Intents))
	}
}
