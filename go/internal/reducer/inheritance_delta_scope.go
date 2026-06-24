// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type inheritanceDeltaScope struct {
	repositoryIDs     []string
	filePathsByRepoID map[string][]string
	hasDelta          bool
}

func loadInheritanceMaterializationFacts(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	_, hasKindLoader := loader.(factKindLoader)
	_, hasPayloadLoader := loader.(factPayloadValueLoader)
	if !hasKindLoader && !hasPayloadLoader {
		envelopes, err := loader.ListFacts(ctx, scopeID, generationID)
		if err != nil {
			return nil, classifyFactLoadError(err)
		}
		return envelopes, nil
	}

	repositoryFacts, err := loadFactsForKinds(ctx, loader, scopeID, generationID, []string{factKindRepository})
	if err != nil {
		return nil, err
	}
	contentFacts, err := loadFactsForKindAndPayloadValue(
		ctx,
		loader,
		scopeID,
		generationID,
		factKindContentEntity,
		"entity_type",
		inheritanceContentEntityTypes,
	)
	if err != nil {
		return nil, err
	}
	return deduplicateInheritanceEnvelopes(append(repositoryFacts, contentFacts...)), nil
}

func deduplicateInheritanceEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	if len(envelopes) < 2 {
		return envelopes
	}
	out := make([]facts.Envelope, 0, len(envelopes))
	seen := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		key := inheritanceEnvelopeKey(envelope)
		if key == "" {
			out = append(out, envelope)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, envelope)
	}
	return out
}

func inheritanceEnvelopeKey(envelope facts.Envelope) string {
	if envelope.FactID != "" {
		return "fact_id:" + envelope.FactID
	}
	if envelope.StableFactKey != "" {
		return "stable:" + envelope.FactKind + ":" + envelope.StableFactKey
	}
	switch envelope.FactKind {
	case factKindRepository:
		repositoryID := semanticPayloadString(envelope.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = semanticPayloadString(envelope.Payload, "graph_id")
		}
		if repositoryID != "" {
			return factKindRepository + ":" + repositoryID
		}
	case factKindContentEntity:
		entityID := semanticPayloadString(envelope.Payload, "entity_id")
		if entityID != "" {
			return factKindContentEntity + ":" + entityID
		}
	}
	return ""
}

func buildInheritanceDeltaScope(envelopes []facts.Envelope) inheritanceDeltaScope {
	seenRepoIDs := make(map[string]struct{})
	seenPathsByRepoID := make(map[string]map[string]struct{})
	scope := inheritanceDeltaScope{}
	for _, env := range envelopes {
		if env.FactKind != factKindRepository || !semanticDeltaPayloadBool(env.Payload, "delta_generation") {
			continue
		}
		repositoryID := semanticPayloadString(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = semanticPayloadString(env.Payload, "graph_id")
		}
		if repositoryID == "" {
			continue
		}
		scope.hasDelta = true
		if _, ok := seenRepoIDs[repositoryID]; !ok {
			seenRepoIDs[repositoryID] = struct{}{}
			scope.repositoryIDs = append(scope.repositoryIDs, repositoryID)
		}
		repoPath := semanticPayloadString(env.Payload, "path")
		if repoPath == "" {
			repoPath = semanticPayloadString(env.Payload, "local_path")
		}
		for _, relativePath := range inheritanceDeltaRelativePaths(env.Payload) {
			filePath := semanticQualifyDeltaPath(repoPath, relativePath)
			if filePath == "" {
				continue
			}
			seen := seenPathsByRepoID[repositoryID]
			if seen == nil {
				seen = make(map[string]struct{})
				seenPathsByRepoID[repositoryID] = seen
			}
			seen[filePath] = struct{}{}
		}
	}
	sort.Strings(scope.repositoryIDs)
	if len(seenPathsByRepoID) == 0 {
		return scope
	}

	scope.filePathsByRepoID = make(map[string][]string, len(seenPathsByRepoID))
	for repositoryID, seen := range seenPathsByRepoID {
		filePaths := make([]string, 0, len(seen))
		for filePath := range seen {
			filePaths = append(filePaths, filePath)
		}
		sort.Strings(filePaths)
		scope.filePathsByRepoID[repositoryID] = filePaths
	}
	return scope
}

func inheritanceDeltaRelativePaths(payload map[string]any) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, key := range []string{"delta_relative_paths", "delta_deleted_relative_paths"} {
		for _, relativePath := range semanticPayloadStringSlice(payload, key) {
			if _, ok := seen[relativePath]; ok {
				continue
			}
			seen[relativePath] = struct{}{}
			paths = append(paths, relativePath)
		}
	}
	return paths
}

func mergeInheritanceRepositoryIDs(repositoryIDs []string, extraRepositoryIDs []string) []string {
	if len(extraRepositoryIDs) == 0 {
		return repositoryIDs
	}
	seen := make(map[string]struct{}, len(repositoryIDs)+len(extraRepositoryIDs))
	merged := make([]string, 0, len(repositoryIDs)+len(extraRepositoryIDs))
	for _, repositoryID := range append(repositoryIDs, extraRepositoryIDs...) {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		if _, ok := seen[repositoryID]; ok {
			continue
		}
		seen[repositoryID] = struct{}{}
		merged = append(merged, repositoryID)
	}
	sort.Strings(merged)
	return merged
}

func buildInheritanceRetractRows(
	repositoryIDs []string,
	deltaScope inheritanceDeltaScope,
) []SharedProjectionIntentRow {
	if len(repositoryIDs) == 0 {
		return nil
	}
	if deltaScope.hasDelta {
		return buildInheritanceDeltaRetractRows(repositoryIDs, deltaScope.filePathsByRepoID)
	}
	return buildInheritanceRepoRetractRows(repositoryIDs)
}

func buildInheritanceRepoRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{RepositoryID: repositoryID})
	}
	return rows
}

func buildInheritanceDeltaRetractRows(
	repositoryIDs []string,
	filePathsByRepoID map[string][]string,
) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			RepositoryID: repositoryID,
			Payload: map[string]any{
				"repo_id":          repositoryID,
				"delta_projection": true,
				"delta_file_paths": append([]string(nil), filePathsByRepoID[repositoryID]...),
			},
		})
	}
	return rows
}
