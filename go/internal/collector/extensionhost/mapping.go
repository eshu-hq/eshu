// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func (s *Source) collectedGeneration(
	item workflow.WorkItem,
	result sdkcollector.Result,
	envelopes []facts.Envelope,
) collector.CollectedGeneration {
	ingestedAt := s.clock().UTC()
	if ingestedAt.Before(result.Generation.ObservedAt) {
		ingestedAt = result.Generation.ObservedAt.UTC()
	}
	scopeValue := scope.IngestionScope{
		ScopeID:       item.ScopeID,
		SourceSystem:  item.SourceSystem,
		ScopeKind:     s.scopeKind,
		CollectorKind: item.CollectorKind,
		PartitionKey:  partitionKey(item),
		Metadata: map[string]string{
			"component_id": s.manifest.Metadata.ID,
			"instance_id":  s.collectorInstanceID,
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID:  result.Generation.ID,
		ScopeID:       item.ScopeID,
		ObservedAt:    result.Generation.ObservedAt.UTC(),
		IngestedAt:    ingestedAt,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: result.Generation.FreshnessHint,
	}
	return collector.FactsFromSlice(scopeValue, generation, envelopes)
}

func (s *Source) envelopesForResult(item workflow.WorkItem, result sdkcollector.Result) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, len(result.Facts))
	seen := make(map[string]struct{}, len(result.Facts))
	for _, fact := range result.Facts {
		key := fact.Kind + "\x00" + fact.StableKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		envelopes = append(envelopes, factenvelope.InternalFromSDKFact(fact, factenvelope.InternalEnvelopeOptions{
			ComponentID:   s.manifest.Metadata.ID,
			ScopeID:       item.ScopeID,
			GenerationID:  item.GenerationID,
			CollectorKind: string(item.CollectorKind),
			FencingToken:  item.CurrentFencingToken,
		}))
	}
	return envelopes
}

func partitionKey(item workflow.WorkItem) string {
	if strings.TrimSpace(item.FairnessKey) != "" {
		return item.FairnessKey
	}
	return item.SourceSystem + ":" + item.ScopeID
}

func cloneConfig(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
