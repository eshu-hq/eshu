// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// scanProjectorWork decodes one claimed projector work row (scope, active
// generation metadata, and payload-derived scope metadata) into a
// projector.ScopeGenerationWork. Split out of projector_queue.go to keep that
// file under the repo's 500-line cap (see also projector_queue_config_state_drift_trigger_hook.go).
func scanProjectorWork(rows Rows) (projector.ScopeGenerationWork, error) {
	var work projector.ScopeGenerationWork
	var scopeKind string
	var collectorKind string
	var generationStatus string
	var triggerKind string
	var rawPayload []byte

	if err := rows.Scan(
		&work.Scope.ScopeID,
		&work.Scope.SourceSystem,
		&scopeKind,
		&work.Scope.ParentScopeID,
		&work.Scope.ActiveGenerationID,
		&work.Scope.PreviousGenerationExists,
		&collectorKind,
		&work.Scope.PartitionKey,
		&work.Generation.GenerationID,
		&work.AttemptCount,
		&work.Generation.ObservedAt,
		&work.Generation.IngestedAt,
		&generationStatus,
		&triggerKind,
		&work.Generation.FreshnessHint,
		&rawPayload,
	); err != nil {
		return projector.ScopeGenerationWork{}, err
	}

	work.Scope.ScopeKind = scope.ScopeKind(scopeKind)
	work.Scope.CollectorKind = scope.CollectorKind(collectorKind)
	work.Generation.ScopeID = work.Scope.ScopeID
	work.Generation.Status = scope.GenerationStatus(generationStatus)
	work.Generation.TriggerKind = scope.TriggerKind(triggerKind)
	work.Generation.ObservedAt = work.Generation.ObservedAt.UTC()
	work.Generation.IngestedAt = work.Generation.IngestedAt.UTC()
	work.Scope.Metadata = projectorScopeMetadata(rawPayload)

	return work, nil
}

func projectorWorkItemID(scopeID string, generationID string) string {
	return fmt.Sprintf("projector_%s_%s", scopeID, generationID)
}

func projectorScopeMetadata(rawPayload []byte) map[string]string {
	payload, err := unmarshalPayload(rawPayload)
	if err != nil || len(payload) == 0 {
		return nil
	}

	metadata := make(map[string]string, len(payload))
	for key, value := range payload {
		switch typed := value.(type) {
		case string:
			if typed != "" {
				metadata[key] = typed
			}
		case fmt.Stringer:
			text := typed.String()
			if text != "" {
				metadata[key] = text
			}
		}
	}
	if len(metadata) == 0 {
		return nil
	}

	return metadata
}
