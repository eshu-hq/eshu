// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package platformfam

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

// PostgresPlatformMaterializationWriter persists one platform-materialization
// reducer reconciliation into the shared fact store.
type PostgresPlatformMaterializationWriter struct {
	DB  factwrite.Execer
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

	now := factwrite.Now(w.Now)
	canonicalID := canonicalPlatformMaterializationID(write)
	payloadJSON, err := json.Marshal(platformMaterializationPayload(write, canonicalID))
	if err != nil {
		return PlatformMaterializationWriteResult{}, fmt.Errorf("marshal platform materialization payload: %w", err)
	}

	if _, err := w.DB.ExecContext(
		ctx,
		factwrite.CanonicalFactInsertQuery,
		write.IntentID,
		write.ScopeID,
		write.GenerationID,
		reducercontract.PlatformMaterializationFactKind,
		platformMaterializationStableFactKey(write),
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
	entityKeys := payloadcore.UniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := payloadcore.UniqueSortedStrings(write.RelatedScopeIDs)
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
	entityKeys := payloadcore.UniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := payloadcore.UniqueSortedStrings(write.RelatedScopeIDs)
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
		"reducer_domain":    string(reducercontract.DomainDeploymentMapping),
		"intent_id":         write.IntentID,
		"scope_id":          write.ScopeID,
		"generation_id":     write.GenerationID,
		"source_system":     write.SourceSystem,
		"cause":             write.Cause,
		"entity_keys":       payloadcore.UniqueSortedStrings(write.EntityKeys),
		"related_scope_ids": payloadcore.UniqueSortedStrings(write.RelatedScopeIDs),
		"canonical_id":      canonicalID,
	}
}
