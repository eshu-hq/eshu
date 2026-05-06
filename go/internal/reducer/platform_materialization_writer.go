package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PostgresPlatformMaterializationWriter persists one platform-materialization
// reducer reconciliation into the shared fact store.
type PostgresPlatformMaterializationWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WritePlatformMaterialization stores one canonical platform-materialization
// fact record.
func (w PostgresPlatformMaterializationWriter) WritePlatformMaterialization(
	ctx context.Context,
	write PlatformMaterializationWrite,
) (PlatformMaterializationWriteResult, error) {
	if w.DB == nil {
		return PlatformMaterializationWriteResult{}, fmt.Errorf("platform materialization database is required")
	}

	now := reducerWriterNow(w.Now)
	canonicalID := canonicalPlatformMaterializationID(write)
	payloadJSON, err := json.Marshal(platformMaterializationPayload(write, canonicalID))
	if err != nil {
		return PlatformMaterializationWriteResult{}, fmt.Errorf("marshal platform materialization payload: %w", err)
	}

	if _, err := w.DB.ExecContext(
		ctx,
		canonicalReducerFactInsertQuery,
		write.IntentID,
		write.ScopeID,
		write.GenerationID,
		"reducer_platform_materialization",
		platformMaterializationStableFactKey(write),
		write.SourceSystem,
		write.IntentID,
		nil,
		nil,
		now,
		now,
		false,
		payloadJSON,
	); err != nil {
		return PlatformMaterializationWriteResult{}, fmt.Errorf("write platform materialization fact: %w", err)
	}

	return PlatformMaterializationWriteResult{
		CanonicalID:     canonicalID,
		CanonicalWrites: 1,
		EvidenceSummary: fmt.Sprintf(
			"wrote platform materialization canonical fact %s",
			canonicalID,
		),
	}, nil
}

func platformMaterializationStableFactKey(write PlatformMaterializationWrite) string {
	entityKeys := uniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := uniqueSortedStrings(write.RelatedScopeIDs)
	parts := []string{
		"platform_materialization",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		strings.Join(entityKeys, "|"),
		strings.Join(relatedScopeIDs, "|"),
	}

	return strings.Join(parts, ":")
}

func canonicalPlatformMaterializationID(write PlatformMaterializationWrite) string {
	entityKeys := uniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := uniqueSortedStrings(write.RelatedScopeIDs)
	parts := []string{
		"platform_materialization",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		strings.TrimSpace(write.SourceSystem),
		strings.Join(entityKeys, "|"),
		strings.Join(relatedScopeIDs, "|"),
	}

	return "canonical:" + strings.Join(parts, ":")
}

func platformMaterializationPayload(
	write PlatformMaterializationWrite,
	canonicalID string,
) map[string]any {
	return map[string]any{
		"reducer_domain":    string(DomainDeploymentMapping),
		"intent_id":         write.IntentID,
		"scope_id":          write.ScopeID,
		"generation_id":     write.GenerationID,
		"source_system":     write.SourceSystem,
		"cause":             write.Cause,
		"entity_keys":       uniqueSortedStrings(write.EntityKeys),
		"related_scope_ids": uniqueSortedStrings(write.RelatedScopeIDs),
		"canonical_id":      canonicalID,
	}
}
