// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudasset

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// PostgresCloudAssetResolutionWriter persists one cloud-asset reducer
// reconciliation into the shared fact store.
type PostgresCloudAssetResolutionWriter struct {
	DB  factwrite.Execer
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

	now := factwrite.Now(w.Now)
	canonicalID := canonicalCloudAssetResolutionID(write)
	payloadJSON, err := json.Marshal(cloudAssetResolutionPayload(write, canonicalID))
	if err != nil {
		return CloudAssetResolutionWriteResult{}, fmt.Errorf("marshal cloud asset resolution payload: %w", err)
	}

	if _, err := w.DB.ExecContext(
		ctx,
		factwrite.SingleInsertQuery,
		write.IntentID,
		write.ScopeID,
		write.GenerationID,
		"reducer_cloud_asset_resolution",
		cloudAssetResolutionStableFactKey(write),
		factwrite.CollectorKind(write.SourceSystem),
		facts.SourceConfidenceInferred,
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
		ReconciledScopes: len(payloadcore.UniqueSortedStrings(write.RelatedScopeIDs)),
		EvidenceSummary: fmt.Sprintf(
			"wrote cloud asset canonical fact %s",
			canonicalID,
		),
	}, nil
}

func cloudAssetResolutionStableFactKey(write CloudAssetResolutionWrite) string {
	entityKeys := payloadcore.UniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := payloadcore.UniqueSortedStrings(write.RelatedScopeIDs)
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
	entityKeys := payloadcore.UniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := payloadcore.UniqueSortedStrings(write.RelatedScopeIDs)
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
		"reducer_domain":    string(reducercontract.DomainCloudAssetResolution),
		"intent_id":         write.IntentID,
		"scope_id":          write.ScopeID,
		"generation_id":     write.GenerationID,
		"source_system":     write.SourceSystem,
		"cause":             write.Cause,
		"entity_keys":       payloadcore.UniqueSortedStrings(write.EntityKeys),
		"related_scope_ids": payloadcore.UniqueSortedStrings(write.RelatedScopeIDs),
		"canonical_id":      canonicalID,
		"source_layers": []string{
			"source_declaration",
			"applied_declaration",
			"observed_resource",
		},
	}
}
