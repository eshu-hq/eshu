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

type documentationDeltaScope struct {
	documentIDs []string
	sectionUIDs []string
	hasDelta    bool
}

func loadDocumentationMaterializationFacts(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return loadFactsForKinds(
		ctx,
		loader,
		scopeID,
		generationID,
		[]string{
			factKindRepository,
			facts.DocumentationDocumentFactKind,
			facts.DocumentationEntityMentionFactKind,
		},
	)
}

func buildDocumentationDeltaScope(envelopes []facts.Envelope, scopeID string) documentationDeltaScope {
	scope := documentationDeltaScope{}
	changedPathsByRepoID := make(map[string]map[string]struct{})
	deletedPathsByRepoID := make(map[string]map[string]struct{})
	changedCandidateDocumentIDs := make(map[string]struct{})
	changedDocumentIDs := make(map[string]struct{})
	seenDocumentIDs := make(map[string]struct{})

	addDocumentID := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seenDocumentIDs[value]; ok {
			return
		}
		seenDocumentIDs[value] = struct{}{}
		scope.documentIDs = append(scope.documentIDs, value)
	}

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
		for _, relativePath := range documentationDeltaRelativePaths(env.Payload, "delta_relative_paths") {
			cleaned := cleanDocumentationDeltaRelativePath(relativePath)
			if cleaned == "" {
				continue
			}
			addDocumentationDeltaPath(changedPathsByRepoID, repositoryID, cleaned)
			changedCandidateDocumentIDs[documentationGitDocumentID(repositoryID, cleaned)] = struct{}{}
		}
		for _, relativePath := range documentationDeltaRelativePaths(env.Payload, "delta_deleted_relative_paths") {
			cleaned := cleanDocumentationDeltaRelativePath(relativePath)
			if cleaned == "" {
				continue
			}
			addDocumentationDeltaPath(deletedPathsByRepoID, repositoryID, cleaned)
			addDocumentID(documentationGitDocumentID(repositoryID, cleaned))
		}
	}
	if !scope.hasDelta {
		return documentationDeltaScope{}
	}

	for _, env := range envelopes {
		if env.FactKind != facts.DocumentationDocumentFactKind || env.IsTombstone {
			continue
		}
		documentID := semanticPayloadString(env.Payload, "document_id")
		if documentID == "" {
			continue
		}
		relativePath := sourceMetadataString(env.Payload, "path")
		if relativePath == "" {
			continue
		}
		repositoryID := sourceMetadataString(env.Payload, "repo_id")
		if repositoryID != "" {
			if documentationDeltaPathMatches(changedPathsByRepoID[repositoryID], relativePath) &&
				strings.HasPrefix(documentID, documentationGitDocumentIDPrefix(repositoryID)) {
				changedDocumentIDs[documentID] = struct{}{}
				addDocumentID(documentID)
			}
			continue
		}
		for candidateRepoID, paths := range changedPathsByRepoID {
			if !strings.HasPrefix(documentID, documentationGitDocumentIDPrefix(candidateRepoID)) {
				continue
			}
			if documentationDeltaPathMatches(paths, relativePath) {
				changedDocumentIDs[documentID] = struct{}{}
				addDocumentID(documentID)
			}
		}
		for candidateRepoID, paths := range deletedPathsByRepoID {
			if !strings.HasPrefix(documentID, documentationGitDocumentIDPrefix(candidateRepoID)) {
				continue
			}
			if documentationDeltaPathMatches(paths, relativePath) {
				addDocumentID(documentID)
			}
		}
	}

	for documentID := range changedCandidateDocumentIDs {
		if _, ok := changedDocumentIDs[documentID]; !ok {
			changedDocumentIDs[documentID] = struct{}{}
		}
		addDocumentID(documentID)
	}

	sort.Strings(scope.documentIDs)
	sort.Strings(scope.sectionUIDs)
	return scope
}

func documentationDeltaRelativePaths(payload map[string]any, key string) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, relativePath := range semanticPayloadStringSlice(payload, key) {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}
	return paths
}

func addDocumentationDeltaPath(pathsByRepoID map[string]map[string]struct{}, repositoryID string, relativePath string) {
	seen := pathsByRepoID[repositoryID]
	if seen == nil {
		seen = make(map[string]struct{})
		pathsByRepoID[repositoryID] = seen
	}
	seen[relativePath] = struct{}{}
}

func cleanDocumentationDeltaRelativePath(relativePath string) string {
	relativePath = strings.TrimSpace(relativePath)
	if relativePath == "" {
		return ""
	}
	cleaned := path.Clean(relativePath)
	if cleaned == "" || cleaned == "." || cleaned == ".." ||
		path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func documentationDeltaPathMatches(paths map[string]struct{}, relativePath string) bool {
	cleaned := cleanDocumentationDeltaRelativePath(relativePath)
	if cleaned == "" {
		return false
	}
	_, ok := paths[cleaned]
	return ok
}

func documentationGitDocumentID(repositoryID string, relativePath string) string {
	return documentationGitDocumentIDPrefix(repositoryID) + relativePath
}

func documentationGitDocumentIDPrefix(repositoryID string) string {
	return "doc:git:" + repositoryID + ":"
}

func sourceMetadataString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	metadata, ok := payload["source_metadata"].(map[string]any)
	if ok {
		return strings.TrimSpace(anyToString(metadata[key]))
	}
	typed, ok := payload["source_metadata"].(map[string]string)
	if ok {
		return strings.TrimSpace(typed[key])
	}
	return ""
}

func buildDocumentationRetractRows(
	scopeIDs []string,
	deltaScope documentationDeltaScope,
) []SharedProjectionIntentRow {
	if len(scopeIDs) == 0 {
		return nil
	}
	if deltaScope.hasDelta {
		return buildDocumentationDeltaRetractRows(scopeIDs, deltaScope.documentIDs, deltaScope.sectionUIDs)
	}
	return buildDocumentationScopeRetractRows(scopeIDs)
}

func buildDocumentationScopeRetractRows(scopeIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		scopeID = strings.TrimSpace(scopeID)
		if scopeID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			ScopeID:      scopeID,
			RepositoryID: scopeID,
			Payload:      map[string]any{"scope_id": scopeID},
		})
	}
	return rows
}

func buildDocumentationDeltaRetractRows(
	scopeIDs []string,
	documentIDs []string,
	sectionUIDs []string,
) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		scopeID = strings.TrimSpace(scopeID)
		if scopeID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			ScopeID:      scopeID,
			RepositoryID: scopeID,
			Payload: map[string]any{
				"scope_id":          scopeID,
				"delta_projection":  true,
				"document_ids":      append([]string(nil), documentIDs...),
				"section_uids":      append([]string(nil), sectionUIDs...),
				"scope_granularity": "document_section",
			},
		})
	}
	return rows
}
