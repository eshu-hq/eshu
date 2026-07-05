// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	documentationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1"
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

// buildDocumentationDeltaScope keeps its pre-typing signature (no quarantine
// slice, no error) because it is the entry point
// documentation_edge_materialization_test.go exercises directly; it
// delegates to buildDocumentationDeltaScopeWithQuarantine and discards the
// quarantine/error results, mirroring ExtractDocumentationEdgeRows's
// identical delegation pattern. The reducer intent path (Handle) calls the
// quarantine-aware function directly so it can report quarantines. A fatal
// (non-input_invalid) decode error is silently dropped here — Handle never
// reaches this path because it calls the WithQuarantine function itself, so
// this wrapper exists solely for the pre-existing direct-call test.
func buildDocumentationDeltaScope(envelopes []facts.Envelope, scopeID string) documentationDeltaScope {
	scope, _, _ := buildDocumentationDeltaScopeWithQuarantine(envelopes, scopeID)
	return scope
}

// buildDocumentationDeltaScopeWithQuarantine is the typed-decode counterpart
// of buildDocumentationDeltaScope (Contract System v1 Wave 4e): it decodes
// each documentation_document envelope through the sdk/go/factschema seam
// (decodeDocumentationDocument) instead of raw semanticPayloadString map
// lookups. A document fact missing its required document_id field is
// quarantined per-fact via partitionDecodeFailures rather than the
// pre-typing behavior of silently excluding it from delta tracking via the
// `if documentID == "" { continue }` check (a missing key and a
// present-but-empty key were indistinguishable, and neither produced any
// operator signal). A non-input_invalid decode error (an unsupported schema
// major) is returned as a fatal error, failing the whole intent for durable
// triage rather than being silently skipped.
func buildDocumentationDeltaScopeWithQuarantine(
	envelopes []facts.Envelope,
	scopeID string,
) (documentationDeltaScope, []quarantinedFact, error) {
	scope := documentationDeltaScope{}
	var quarantined []quarantinedFact
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
		return documentationDeltaScope{}, nil, nil
	}

	for _, env := range envelopes {
		if env.FactKind != facts.DocumentationDocumentFactKind || env.IsTombstone {
			continue
		}
		document, err := decodeDocumentationDocument(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return documentationDeltaScope{}, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		documentID := document.DocumentID
		if documentID == "" {
			continue
		}
		relativePath := documentSourceMetadataString(document, "path")
		if relativePath == "" {
			continue
		}
		repositoryID := documentSourceMetadataString(document, "repo_id")
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
	return scope, quarantined, nil
}

// documentSourceMetadataString reads a key out of a decoded
// documentationv1.Document's optional SourceMetadata map, returning "" for a
// nil map or an absent key. It mirrors the pre-typing sourceMetadataString
// helper's behavior over the raw payload map, now reading the typed
// map[string]string field instead of a map[string]any/map[string]string
// type-switch.
func documentSourceMetadataString(document documentationv1.Document, key string) string {
	if document.SourceMetadata == nil {
		return ""
	}
	return strings.TrimSpace(document.SourceMetadata[key])
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
