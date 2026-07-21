// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// codeownersOwnershipDeltaScope mirrors inheritanceDeltaScope
// (inheritance_delta_scope.go): a CODEOWNERS file is repo-scoped just like a
// source file, so a changed or deleted CODEOWNERS source_path retracts the
// prior generation's DECLARES_CODEOWNER edges scoped to that path rather than
// sweeping the whole repository.
type codeownersOwnershipDeltaScope struct {
	repositoryIDs     []string
	filePathsByRepoID map[string][]string
	hasDelta          bool
}

// loadCodeownersOwnershipMaterializationFacts loads the repository delta facts
// plus every codeowners.ownership fact for the generation. codeowners.ownership
// is a directly-emitted fact (Contract System v1), so no content_entity/
// parsed_file_data join is needed the way inheritance needs content_entity.
func loadCodeownersOwnershipMaterializationFacts(
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
		[]string{factKindRepository, factKindCodeownersOwnership},
	)
}

// buildCodeownersOwnershipDeltaScope mirrors buildInheritanceDeltaScope: it scans
// repository facts flagged delta_generation and collects every changed/deleted
// relative path per repository. The retract Cypher filters
// `rel.source_path IN $delta_file_paths`, so passing every changed path
// (not just CODEOWNERS files) is harmless: a changed path that is not a
// CODEOWNERS source_path simply matches no edge.
func buildCodeownersOwnershipDeltaScope(envelopes []facts.Envelope) codeownersOwnershipDeltaScope {
	seenRepoIDs := make(map[string]struct{})
	seenPathsByRepoID := make(map[string]map[string]struct{})
	scope := codeownersOwnershipDeltaScope{}
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
		for _, relativePath := range codeownersOwnershipDeltaRelativePaths(env.Payload) {
			cleaned := strings.TrimSpace(relativePath)
			if cleaned == "" {
				continue
			}
			seen := seenPathsByRepoID[repositoryID]
			if seen == nil {
				seen = make(map[string]struct{})
				seenPathsByRepoID[repositoryID] = seen
			}
			seen[cleaned] = struct{}{}
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

func codeownersOwnershipDeltaRelativePaths(payload map[string]any) []string {
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

// buildCodeownersOwnershipRetractRows builds the shared-projection retract rows
// for the given repositories. When the generation carries delta scope, each row
// scopes the retract to the repo's changed/deleted relative paths via
// delta_file_paths (read generically by cypher.collectDeltaFilePaths, the same
// mechanism code_calls/inheritance/shell_exec/sql_relationships use); otherwise
// each row requests a whole-repository retract, matching
// buildInheritanceRetractRows.
func buildCodeownersOwnershipRetractRows(
	repositoryIDs []string,
	deltaScope codeownersOwnershipDeltaScope,
) []SharedProjectionIntentRow {
	if len(repositoryIDs) == 0 {
		return nil
	}
	if deltaScope.hasDelta {
		return buildCodeownersOwnershipDeltaRetractRows(repositoryIDs, deltaScope.filePathsByRepoID)
	}
	return buildCodeownersOwnershipRepoRetractRows(repositoryIDs)
}

func buildCodeownersOwnershipRepoRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
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

func buildCodeownersOwnershipDeltaRetractRows(
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
