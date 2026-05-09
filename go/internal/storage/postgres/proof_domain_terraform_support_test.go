package postgres

import (
	"encoding/json"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func cloneEvidenceFacts(input map[string]evidenceRecord) map[string]evidenceRecord {
	cloned := make(map[string]evidenceRecord, len(input))
	for key, value := range input {
		if value.details != nil {
			details := make(map[string]any, len(value.details))
			for detailKey, detailValue := range value.details {
				details[detailKey] = detailValue
			}
			value.details = details
		}
		cloned[key] = value
	}
	return cloned
}

func proofFactRows(input map[string]facts.Envelope, scopeID, generationID string) [][]any {
	rows := [][]any{}
	for _, envelope := range input {
		if envelope.ScopeID != scopeID || envelope.GenerationID != generationID {
			continue
		}
		payload, _ := json.Marshal(envelope.Payload)
		rows = append(rows, proofFactEnvelopeRow(envelope, payload))
	}
	return rows
}

func proofRepositoryCatalogRows(input map[string]facts.Envelope) [][]any {
	rows := make([][]any, 0, len(input))
	seen := make(map[string]struct{})
	for _, envelope := range input {
		if envelope.FactKind != "repository" {
			continue
		}
		repoID := catalogString(envelope.Payload, "repo_id", "graph_id", "name")
		if repoID == "" {
			continue
		}
		if _, exists := seen[repoID]; exists {
			continue
		}
		seen[repoID] = struct{}{}
		payload, _ := json.Marshal(envelope.Payload)
		rows = append(rows, []any{payload})
	}
	return rows
}

func proofLatestRelationshipFactRows(state proofState) [][]any {
	latestGenerationByScope := make(map[string]string, len(state.generations))
	for scopeID, activeGenerationID := range state.activeGenerations {
		if activeGenerationID != "" {
			latestGenerationByScope[scopeID] = activeGenerationID
		}
	}
	bestGenerationByScope := make(map[string]scope.ScopeGeneration, len(state.generations))
	for _, generation := range state.generations {
		if latestGenerationByScope[generation.ScopeID] != "" {
			continue
		}
		current, ok := bestGenerationByScope[generation.ScopeID]
		if ok && laterGeneration(current, generation) {
			continue
		}
		bestGenerationByScope[generation.ScopeID] = generation
		latestGenerationByScope[generation.ScopeID] = generation.GenerationID
	}

	envelopes := make([]facts.Envelope, 0, len(state.facts))
	for _, envelope := range state.facts {
		latestGenerationID, ok := latestGenerationByScope[envelope.ScopeID]
		if !ok || latestGenerationID != envelope.GenerationID {
			continue
		}
		if envelope.FactKind != "content" && envelope.FactKind != "file" {
			continue
		}
		envelopes = append(envelopes, envelope)
	}
	sort.Slice(envelopes, func(i, j int) bool {
		if envelopes[i].ObservedAt.Equal(envelopes[j].ObservedAt) {
			return envelopes[i].FactID < envelopes[j].FactID
		}
		return envelopes[i].ObservedAt.Before(envelopes[j].ObservedAt)
	})

	rows := make([][]any, 0, len(envelopes))
	for _, envelope := range envelopes {
		payload, _ := json.Marshal(envelope.Payload)
		rows = append(rows, proofFactEnvelopeRow(envelope, payload))
	}
	return rows
}

func proofFactEnvelopeRow(envelope facts.Envelope, payload []byte) []any {
	return []any{
		envelope.FactID,
		envelope.ScopeID,
		envelope.GenerationID,
		envelope.FactKind,
		envelope.StableFactKey,
		emptyToDefault(envelope.SchemaVersion, "unknown.v1"),
		emptyToDefault(envelope.CollectorKind, emptyToDefault(envelope.SourceRef.SourceSystem, "unknown")),
		envelope.FencingToken,
		emptyToDefault(envelope.SourceConfidence, "unknown"),
		envelope.SourceRef.SourceSystem,
		envelope.SourceRef.FactKey,
		envelope.SourceRef.SourceURI,
		envelope.SourceRef.SourceRecordID,
		envelope.ObservedAt.UTC(),
		envelope.IsTombstone,
		payload,
	}
}

func laterGeneration(current scope.ScopeGeneration, candidate scope.ScopeGeneration) bool {
	if candidate.IngestedAt.After(current.IngestedAt) {
		return false
	}
	if candidate.IngestedAt.Before(current.IngestedAt) {
		return true
	}
	return candidate.GenerationID <= current.GenerationID
}
