// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type sqlRelationshipDeltaScope struct {
	repositoryIDs     []string
	filePathsByRepoID map[string][]string
	hasDelta          bool
}

func loadSQLRelationshipMaterializationFacts(
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
		sqlRelationshipContentEntityTypes,
	)
	if err != nil {
		return nil, err
	}
	fileFacts, err := loadFactsForKinds(ctx, loader, scopeID, generationID, []string{factKindFile})
	if err != nil {
		return nil, err
	}
	envelopes := append(repositoryFacts, contentFacts...)
	envelopes = append(envelopes, fileFacts...)
	return deduplicateSQLRelationshipEnvelopes(envelopes), nil
}

func deduplicateSQLRelationshipEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	if len(envelopes) < 2 {
		return envelopes
	}
	out := make([]facts.Envelope, 0, len(envelopes))
	seen := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		key := sqlRelationshipEnvelopeKey(envelope)
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

// sqlRelationshipEnvelopeKey builds the dedup key for one envelope.
// "repository" and "file" fact identity is decoded through the codegraph
// contracts seam (decodeCodegraphRepository, decodeCodegraphFile, Contract
// System v1 Wave 4f S2, issue #4754) rather than raw semanticPayloadString
// lookups. A decode failure falls through to the "" no-dedup-key return,
// matching this function's pre-existing behavior for a fact missing its
// identity fields (such a fact was never deduplicated by identity before this
// conversion either — it fell through every case to the FactID/StableFactKey
// checks above, which already ran first).
func sqlRelationshipEnvelopeKey(envelope facts.Envelope) string {
	if envelope.FactID != "" {
		return "fact_id:" + envelope.FactID
	}
	if envelope.StableFactKey != "" {
		return "stable:" + envelope.FactKind + ":" + envelope.StableFactKey
	}
	switch envelope.FactKind {
	case factKindRepository:
		repository, err := decodeCodegraphRepository(envelope)
		if err != nil {
			return ""
		}
		repositoryID := strings.TrimSpace(repository.RepoID)
		if repositoryID == "" && repository.GraphID != nil {
			repositoryID = strings.TrimSpace(*repository.GraphID)
		}
		if repositoryID != "" {
			return factKindRepository + ":" + repositoryID
		}
	case factKindContentEntity:
		entityID := semanticPayloadString(envelope.Payload, "entity_id")
		if entityID != "" {
			return factKindContentEntity + ":" + entityID
		}
	case factKindFile:
		file, err := decodeCodegraphFile(envelope)
		if err != nil {
			return ""
		}
		repositoryID := strings.TrimSpace(file.RepoID)
		relativePath := strings.TrimSpace(file.RelativePath)
		if repositoryID != "" && relativePath != "" {
			return factKindFile + ":" + repositoryID + ":" + relativePath
		}
	}
	return ""
}

// buildSQLRelationshipDeltaScope builds the delta-generation scope from the
// batch's "repository" facts. Repository identity and the delta path slices
// are decoded through the codegraph contracts seam (decodeCodegraphRepository,
// Contract System v1 Wave 4f S2, issue #4754) rather than raw
// semanticPayloadString/semanticPayloadStringSlice lookups. The cheap
// delta_generation gate check runs on the raw payload before decode (matching
// code_call_materialization_intents.go's buildCodeCallDeltaFileScopesByRepoID
// precedent) so a non-delta repository fact never pays the decode cost. A
// delta-generation repository fact whose payload is missing a required
// identity field is skipped, matching this function's pre-existing "skip and
// continue" shape for an absent repo_id.
func buildSQLRelationshipDeltaScope(envelopes []facts.Envelope) sqlRelationshipDeltaScope {
	seenRepoIDs := make(map[string]struct{})
	seenPathsByRepoID := make(map[string]map[string]struct{})
	scope := sqlRelationshipDeltaScope{}
	for _, env := range envelopes {
		if env.FactKind != factKindRepository || !sqlRelationshipPayloadBool(env.Payload, "delta_generation") {
			continue
		}
		repository, err := decodeCodegraphRepository(env)
		if err != nil {
			continue
		}
		repositoryID := strings.TrimSpace(repository.RepoID)
		if repositoryID == "" && repository.GraphID != nil {
			repositoryID = strings.TrimSpace(*repository.GraphID)
		}
		if repositoryID == "" {
			continue
		}
		scope.hasDelta = true
		if _, ok := seenRepoIDs[repositoryID]; !ok {
			seenRepoIDs[repositoryID] = struct{}{}
			scope.repositoryIDs = append(scope.repositoryIDs, repositoryID)
		}
		// "path" is read raw off the top-level envelope first, then falls
		// back to the typed LocalPath — preserving the exact
		// pre-Contract-System precedence documented in
		// code_call_materialization_intents.go's
		// buildCodeCallDeltaFileScopesByRepoID: "path" is NOT a typed
		// codegraphv1.Repository field (repositoryFactEnvelope never writes
		// it to the payload in production), so it is read raw here only to
		// preserve behavior for callers/fixtures that carry the checkout
		// path under "path".
		repoPath := semanticPayloadString(env.Payload, "path")
		if repoPath == "" && repository.LocalPath != nil {
			repoPath = strings.TrimSpace(*repository.LocalPath)
		}
		for _, relativePath := range codeCallDeltaRelativePathsFromRepository(repository) {
			// TrimSpace + skip-empty preserves the pre-Contract-System
			// semanticPayloadStringSlice behavior: that helper trimmed and
			// dropped each blank/whitespace-only path entry BEFORE
			// qualification, so a whitespace-only entry never reached
			// path.Join. codeCallDeltaRelativePathsFromRepository (reused
			// from the codegraph decode seam) returns each JSON array
			// element raw, so the trim must happen here or a whitespace-only
			// delta_relative_paths entry would qualify into a bogus
			// "<repoPath>/  "-shaped path (path.Clean does not trim
			// whitespace).
			relativePath = strings.TrimSpace(relativePath)
			if relativePath == "" {
				continue
			}
			filePath := qualifySQLRelationshipDeltaFilePath(repoPath, relativePath)
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

func qualifySQLRelationshipDeltaFilePath(repoPath string, relativePath string) string {
	if repoPath == "" || relativePath == "" {
		return ""
	}
	cleaned := path.Clean(relativePath)
	if cleaned == "" || cleaned == "." || cleaned == ".." ||
		path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return path.Join(repoPath, cleaned)
}

func mergeSQLRelationshipRepositoryIDs(repositoryIDs []string, extraRepositoryIDs []string) []string {
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

func buildSQLRelationshipRetractRows(
	repositoryIDs []string,
	deltaScope sqlRelationshipDeltaScope,
) []SharedProjectionIntentRow {
	if len(repositoryIDs) == 0 {
		return nil
	}
	if deltaScope.hasDelta {
		return buildSQLRelationshipDeltaRetractRows(repositoryIDs, deltaScope.filePathsByRepoID)
	}
	return buildSQLRelationshipRepoRetractRows(repositoryIDs)
}

func buildSQLRelationshipRepoRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			RepositoryID: repositoryID,
			Payload:      map[string]any{"repo_id": repositoryID},
		})
	}
	return rows
}

func buildSQLRelationshipDeltaRetractRows(
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

func sqlRelationshipPayloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}
