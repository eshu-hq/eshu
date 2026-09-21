// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package stage

import (
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/decode"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// EntityStageResult captures the output of the entity projection stage.
type EntityStageResult struct {
	Entities []content.EntityRecord
}

// ProjectEntityStage projects parsed-entity facts into content entity records.
// It deduplicates by fact ID and builds entity records from payload metadata.
func ProjectEntityStage(repoID string, envelopes []facts.Envelope) EntityStageResult {
	entityFacts := decode.FilterEntityFacts(envelopes)
	result := EntityStageResult{
		Entities: make([]content.EntityRecord, 0, len(entityFacts)),
	}

	for i := range entityFacts {
		if record, ok := runtime.BuildContentEntityRecord(repoID, entityFacts[i]); ok {
			result.Entities = append(result.Entities, record)
		}
	}

	return result
}
