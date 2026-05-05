package projector

import (
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// FileStageResult captures the output of the file projection stage.
type FileStageResult struct {
	ContentRecords []content.Record
	Entities       []content.EntityRecord
}

// ProjectFileStage projects file observation facts into content records and
// content entity records. It deduplicates by fact ID.
func ProjectFileStage(repoID string, envelopes []facts.Envelope) FileStageResult {
	fileFacts := FilterFileFacts(envelopes)
	result := FileStageResult{}

	for i := range fileFacts {
		if record, ok := buildContentRecord(fileFacts[i]); ok {
			result.ContentRecords = append(result.ContentRecords, record)
		}
		if entity, ok := buildContentEntityRecord(repoID, fileFacts[i]); ok {
			result.Entities = append(result.Entities, entity)
		}
	}

	return result
}
