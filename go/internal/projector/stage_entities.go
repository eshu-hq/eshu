package projector

import (
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// EntityStageResult captures the output of the entity projection stage.
type EntityStageResult struct {
	Entities []content.EntityRecord
}

// ProjectEntityStage projects parsed-entity facts into content entity records.
// It deduplicates by fact ID and builds entity records from payload metadata.
func ProjectEntityStage(repoID string, envelopes []facts.Envelope) EntityStageResult {
	entityFacts := FilterEntityFacts(envelopes)
	result := EntityStageResult{
		Entities: make([]content.EntityRecord, 0, len(entityFacts)),
	}

	for i := range entityFacts {
		if record, ok := buildContentEntityRecord(repoID, entityFacts[i]); ok {
			result.Entities = append(result.Entities, record)
		}
	}

	return result
}
