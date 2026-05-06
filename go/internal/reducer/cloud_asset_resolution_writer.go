package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PostgresCloudAssetResolutionWriter persists one cloud-asset reducer
// reconciliation into the shared fact store.
type PostgresCloudAssetResolutionWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteCloudAssetResolution stores one canonical cloud-asset fact record.
func (w PostgresCloudAssetResolutionWriter) WriteCloudAssetResolution(
	ctx context.Context,
	write CloudAssetResolutionWrite,
) (CloudAssetResolutionWriteResult, error) {
	if w.DB == nil {
		return CloudAssetResolutionWriteResult{}, fmt.Errorf("cloud asset resolution database is required")
	}

	now := reducerWriterNow(w.Now)
	canonicalID := canonicalCloudAssetResolutionID(write)
	payloadJSON, err := json.Marshal(cloudAssetResolutionPayload(write, canonicalID))
	if err != nil {
		return CloudAssetResolutionWriteResult{}, fmt.Errorf("marshal cloud asset resolution payload: %w", err)
	}

	if _, err := w.DB.ExecContext(
		ctx,
		canonicalReducerFactInsertQuery,
		write.IntentID,
		write.ScopeID,
		write.GenerationID,
		"reducer_cloud_asset_resolution",
		cloudAssetResolutionStableFactKey(write),
		write.SourceSystem,
		write.IntentID,
		nil,
		nil,
		now,
		now,
		false,
		payloadJSON,
	); err != nil {
		return CloudAssetResolutionWriteResult{}, fmt.Errorf("write cloud asset resolution fact: %w", err)
	}

	return CloudAssetResolutionWriteResult{
		CanonicalID:      canonicalID,
		CanonicalWrites:  1,
		ReconciledScopes: len(uniqueSortedStrings(write.RelatedScopeIDs)),
		EvidenceSummary: fmt.Sprintf(
			"wrote cloud asset canonical fact %s",
			canonicalID,
		),
	}, nil
}

func cloudAssetResolutionStableFactKey(write CloudAssetResolutionWrite) string {
	entityKeys := uniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := uniqueSortedStrings(write.RelatedScopeIDs)
	parts := []string{
		"cloud_asset_resolution",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		strings.Join(entityKeys, "|"),
		strings.Join(relatedScopeIDs, "|"),
	}

	return strings.Join(parts, ":")
}

func canonicalCloudAssetResolutionID(write CloudAssetResolutionWrite) string {
	entityKeys := uniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := uniqueSortedStrings(write.RelatedScopeIDs)
	parts := []string{
		"cloud_asset",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		strings.TrimSpace(write.SourceSystem),
		strings.Join(entityKeys, "|"),
		strings.Join(relatedScopeIDs, "|"),
	}

	return "canonical:" + strings.Join(parts, ":")
}

func cloudAssetResolutionPayload(
	write CloudAssetResolutionWrite,
	canonicalID string,
) map[string]any {
	return map[string]any{
		"reducer_domain":    string(DomainCloudAssetResolution),
		"intent_id":         write.IntentID,
		"scope_id":          write.ScopeID,
		"generation_id":     write.GenerationID,
		"source_system":     write.SourceSystem,
		"cause":             write.Cause,
		"entity_keys":       uniqueSortedStrings(write.EntityKeys),
		"related_scope_ids": uniqueSortedStrings(write.RelatedScopeIDs),
		"canonical_id":      canonicalID,
		"source_layers": []string{
			"source_declaration",
			"applied_declaration",
			"observed_resource",
		},
	}
}
