// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// submodulePinGitmodulesRelativePath is the exact repo-relative ".gitmodules"
// location the git collector recognizes (see
// internal/collector/submodule.IsGitmodulesPath). This is duplicated from
// (not imported from) that collector package to respect the collector/reducer
// package ownership boundary (docs/internal/agent-guide.md#ownership-boundaries);
// the two values MUST stay in lockstep. Unlike CODEOWNERS (three recognized
// candidate locations, see codeownersOwnershipCandidatePaths), git honors
// exactly one ".gitmodules" location per repository, so no candidate list is
// needed here.
const submodulePinGitmodulesRelativePath = ".gitmodules"

// submodulePinDeltaScope mirrors codeownersOwnershipDeltaScope, simplified for
// the single-source-location shape of submodule.pin: a repository whose delta
// touched ".gitmodules" (changed or deleted) is recorded in
// gitmodulesTouchedRepoIDs so its PINS_SUBMODULE edges get a whole-repository
// retract before rewriting; every other delta-flagged repository's submodule
// edges are left untouched (no candidate-switching concern exists with only
// one recognized source file, unlike CODEOWNERS' three locations).
type submodulePinDeltaScope struct {
	repositoryIDs            []string
	gitmodulesTouchedRepoIDs map[string]struct{}
	hasDelta                 bool
}

// loadSubmodulePinMaterializationFacts loads the repository delta facts plus
// every submodule.pin fact for the generation. submodule.pin is a
// directly-emitted fact (Contract System v1), so no content_entity/
// parsed_file_data join is needed.
func loadSubmodulePinMaterializationFacts(
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
		[]string{factKindRepository, factKindSubmodulePin},
	)
}

// buildSubmodulePinDeltaScope scans repository facts flagged delta_generation
// and records, per repository, whether the delta's changed/deleted relative
// paths include ".gitmodules" — the only location submodule.pin facts are
// ever sourced from.
func buildSubmodulePinDeltaScope(envelopes []facts.Envelope) submodulePinDeltaScope {
	seenRepoIDs := make(map[string]struct{})
	scope := submodulePinDeltaScope{}
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
		if submodulePinDeltaTouchesGitmodules(env.Payload) {
			if scope.gitmodulesTouchedRepoIDs == nil {
				scope.gitmodulesTouchedRepoIDs = make(map[string]struct{})
			}
			scope.gitmodulesTouchedRepoIDs[repositoryID] = struct{}{}
		}
	}
	sort.Strings(scope.repositoryIDs)
	return scope
}

// submodulePinDeltaTouchesGitmodules reports whether the repository delta's
// changed or deleted relative paths include ".gitmodules".
func submodulePinDeltaTouchesGitmodules(payload map[string]any) bool {
	for _, key := range []string{"delta_relative_paths", "delta_deleted_relative_paths"} {
		for _, relativePath := range semanticPayloadStringSlice(payload, key) {
			if strings.TrimSpace(relativePath) == submodulePinGitmodulesRelativePath {
				return true
			}
		}
	}
	return false
}

// buildSubmodulePinRetractRows builds the shared-projection retract rows for
// the given repositories. Outside a delta generation, every repository gets a
// whole-repository retract row, matching buildCodeownersOwnershipRetractRows.
// Inside a delta generation, only repositories whose delta touched
// ".gitmodules" get a whole-repository retract row; every other repository is
// skipped entirely (its submodule.pin facts could not have changed this
// generation, so there is nothing to retract). This is simpler than
// CODEOWNERS' delta-aware split (buildCodeownersOwnershipDeltaAwareRetractRows):
// CODEOWNERS needs a path-scoped fallback retract because any of three
// candidate locations could be the prior winner, but submodule.pin has only
// one recognized source location, so an untouched ".gitmodules" means the
// repository's PINS_SUBMODULE edges are provably unaffected.
func buildSubmodulePinRetractRows(
	repositoryIDs []string,
	deltaScope submodulePinDeltaScope,
) []SharedProjectionIntentRow {
	if len(repositoryIDs) == 0 {
		return nil
	}
	if !deltaScope.hasDelta {
		return buildSubmodulePinRepoRetractRows(repositoryIDs)
	}
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		if _, touched := deltaScope.gitmodulesTouchedRepoIDs[repositoryID]; !touched {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{RepositoryID: repositoryID})
	}
	return rows
}

func buildSubmodulePinRepoRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
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
