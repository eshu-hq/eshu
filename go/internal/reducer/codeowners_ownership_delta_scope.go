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

// codeownersOwnershipCandidatePaths lists the exact repo-relative CODEOWNERS
// locations GitHub honors. This is duplicated from (not imported from)
// internal/collector/codeowners.CandidatePaths() to respect the
// collector/reducer package ownership boundary
// (docs/internal/agent-guide.md#ownership-boundaries); the two lists MUST
// stay in lockstep. CODEOWNERS winner-resolution is inherently whole-repo, so
// the reducer needs the same three locations to detect "this delta might have
// changed the winner" (see codeownersOwnershipDeltaTouchesCandidate) and force
// a whole-repository re-projection instead of the ordinary path-scoped delta
// retract (issue #5419 P1).
var codeownersOwnershipCandidatePaths = []string{
	".github/CODEOWNERS",
	"CODEOWNERS",
	"docs/CODEOWNERS",
}

// codeownersOwnershipDeltaTouchesCandidate reports whether any of filePaths is
// one of the three recognized CODEOWNERS candidate locations.
func codeownersOwnershipDeltaTouchesCandidate(filePaths []string) bool {
	for _, filePath := range filePaths {
		for _, candidate := range codeownersOwnershipCandidatePaths {
			if filePath == candidate {
				return true
			}
		}
	}
	return false
}

// buildCodeownersOwnershipRetractRows builds the shared-projection retract rows
// for the given repositories. Outside a delta generation, every row requests a
// whole-repository retract, matching buildInheritanceRetractRows. Inside a
// delta generation, each repository gets either a whole-repository retract
// row (when its delta touched a CODEOWNERS candidate location — see
// buildCodeownersOwnershipDeltaAwareRetractRows) or a row scoped to its
// changed/deleted relative paths via delta_file_paths (read generically by
// cypher.collectDeltaFilePaths, the same mechanism
// code_calls/inheritance/shell_exec/sql_relationships use).
func buildCodeownersOwnershipRetractRows(
	repositoryIDs []string,
	deltaScope codeownersOwnershipDeltaScope,
) []SharedProjectionIntentRow {
	if len(repositoryIDs) == 0 {
		return nil
	}
	if deltaScope.hasDelta {
		return buildCodeownersOwnershipDeltaAwareRetractRows(repositoryIDs, deltaScope.filePathsByRepoID)
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

// buildCodeownersOwnershipDeltaAwareRetractRows builds one retract row per
// repository for a delta generation. CODEOWNERS winner-resolution is
// whole-repo (one winner among three known locations), so a repository whose
// delta touched any of those three locations gets a whole-repository retract
// row (mirroring the non-delta path) instead of the ordinary path-scoped delta
// retract row: the file that changed or was deleted may not be the file whose
// edges need retracting (the winner may have switched to or from a different
// candidate location entirely), so scoping the retract to only the touched
// path would leave the losing or former-winner file's stale edges behind
// (issue #5419 P1: a union of both files' edges, or an empty graph when the
// new winner emits under a source_path the scoped retract never considered).
// A repository whose delta touched no CODEOWNERS candidate keeps the ordinary
// path-scoped retract, which stays a harmless no-op against DECLARES_CODEOWNER
// edges when the touched paths are not CODEOWNERS source paths.
func buildCodeownersOwnershipDeltaAwareRetractRows(
	repositoryIDs []string,
	filePathsByRepoID map[string][]string,
) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		filePaths := filePathsByRepoID[repositoryID]
		if codeownersOwnershipDeltaTouchesCandidate(filePaths) {
			rows = append(rows, SharedProjectionIntentRow{RepositoryID: repositoryID})
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			RepositoryID: repositoryID,
			Payload: map[string]any{
				"repo_id":          repositoryID,
				"delta_projection": true,
				"delta_file_paths": append([]string(nil), filePaths...),
			},
		})
	}
	return rows
}
