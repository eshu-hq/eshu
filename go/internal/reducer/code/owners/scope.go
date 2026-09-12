// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package owners

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// deltaScope mirrors the inheritance family's delta scope: a CODEOWNERS file
// is repo-scoped just like a source file, so a changed or deleted CODEOWNERS
// source_path retracts the prior generation's DECLARES_CODEOWNER edges scoped
// to that path rather than sweeping the whole repository.
type deltaScope struct {
	repositoryIDs     []string
	filePathsByRepoID map[string][]string
	hasDelta          bool
}

// materializationFactKinds is the single source for the kind set
// LoadMaterializationFacts requests; the reducer root's
// factload_materialization_bench_test.go corpus guard reads the same slice
// (through MaterializationFactKinds) so a newly requested kind without seeded
// coverage fails there, not silently.
var materializationFactKinds = []string{factload.FactKindRepository, factload.FactKindCodeownersOwnership}

// MaterializationFactKinds returns the fact kinds codeowners ownership
// materialization requests, as a fresh copy so a caller cannot mutate the
// package's backing slice. Exported: the reducer root's
// factload_materialization_bench_test.go corpus-coverage guard reads it
// through the codeownersMaterializationFactKinds compat forwarder.
func MaterializationFactKinds() []string {
	out := make([]string, len(materializationFactKinds))
	copy(out, materializationFactKinds)
	return out
}

// LoadMaterializationFacts loads the repository delta facts plus every
// codeowners.ownership fact for the generation. codeowners.ownership is a
// directly-emitted fact (Contract System v1), so no content_entity/
// parsed_file_data join is needed the way inheritance needs content_entity.
func LoadMaterializationFacts(
	ctx context.Context,
	loader factload.FactLoader,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return factload.LoadFactsForKinds(
		ctx,
		loader,
		scopeID,
		generationID,
		materializationFactKinds,
	)
}

// buildDeltaScope mirrors inheritance.BuildDeltaScope: it scans repository
// facts flagged delta_generation and collects every changed/deleted relative
// path per repository. The retract Cypher filters `rel.source_path IN
// $delta_file_paths`, so passing every changed path (not just CODEOWNERS
// files) is harmless: a changed path that is not a CODEOWNERS source_path
// simply matches no edge.
func buildDeltaScope(envelopes []facts.Envelope) deltaScope {
	seenRepoIDs := make(map[string]struct{})
	seenPathsByRepoID := make(map[string]map[string]struct{})
	scope := deltaScope{}
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindRepository || !payloadcore.DeltaPayloadBool(env.Payload, "delta_generation") {
			continue
		}
		repositoryID := payloadcore.SemanticPayloadString(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = payloadcore.SemanticPayloadString(env.Payload, "graph_id")
		}
		if repositoryID == "" {
			continue
		}
		scope.hasDelta = true
		if _, ok := seenRepoIDs[repositoryID]; !ok {
			seenRepoIDs[repositoryID] = struct{}{}
			scope.repositoryIDs = append(scope.repositoryIDs, repositoryID)
		}
		for _, relativePath := range deltaRelativePaths(env.Payload) {
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

func deltaRelativePaths(payload map[string]any) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, key := range []string{"delta_relative_paths", "delta_deleted_relative_paths"} {
		for _, relativePath := range payloadcore.SemanticPayloadStringSlice(payload, key) {
			if _, ok := seen[relativePath]; ok {
				continue
			}
			seen[relativePath] = struct{}{}
			paths = append(paths, relativePath)
		}
	}
	return paths
}

// candidatePaths lists the exact repo-relative CODEOWNERS locations GitHub
// honors. This is duplicated from (not imported from)
// internal/collector/codeowners.CandidatePaths() to respect the
// collector/reducer package ownership boundary
// (docs/internal/agent-guide.md#ownership-boundaries); the two lists MUST
// stay in lockstep. CODEOWNERS winner-resolution is inherently whole-repo, so
// the reducer needs the same three locations to detect "this delta might have
// changed the winner" (see deltaTouchesCandidate) and force a
// whole-repository re-projection instead of the ordinary path-scoped delta
// retract (issue #5419 P1).
var candidatePaths = []string{
	".github/CODEOWNERS",
	"CODEOWNERS",
	"docs/CODEOWNERS",
}

// deltaTouchesCandidate reports whether any of filePaths is one of the three
// recognized CODEOWNERS candidate locations.
func deltaTouchesCandidate(filePaths []string) bool {
	for _, filePath := range filePaths {
		for _, candidate := range candidatePaths {
			if filePath == candidate {
				return true
			}
		}
	}
	return false
}

// buildRetractRows builds the shared-projection retract rows for the given
// repositories. Outside a delta generation, every row requests a
// whole-repository retract, matching inheritance.BuildRetractRows. Inside a
// delta generation, each repository gets either a whole-repository retract
// row (when its delta touched a CODEOWNERS candidate location — see
// buildDeltaAwareRetractRows) or a row scoped to its changed/deleted relative
// paths via delta_file_paths (read generically by cypher.collectDeltaFilePaths,
// the same mechanism code_calls/inheritance/shell_exec/sql_relationships use).
func buildRetractRows(
	repositoryIDs []string,
	scope deltaScope,
) []sharedintent.Row {
	if len(repositoryIDs) == 0 {
		return nil
	}
	if scope.hasDelta {
		return buildDeltaAwareRetractRows(repositoryIDs, scope.filePathsByRepoID)
	}
	return buildRepoRetractRows(repositoryIDs)
}

func buildRepoRetractRows(repositoryIDs []string) []sharedintent.Row {
	rows := make([]sharedintent.Row, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		rows = append(rows, sharedintent.Row{RepositoryID: repositoryID})
	}
	return rows
}

// buildDeltaAwareRetractRows builds one retract row per repository for a
// delta generation. CODEOWNERS winner-resolution is whole-repo (one winner
// among three known locations), so a repository whose delta touched any of
// those three locations gets a whole-repository retract row (mirroring the
// non-delta path) instead of the ordinary path-scoped delta retract row: the
// file that changed or was deleted may not be the file whose edges need
// retracting (the winner may have switched to or from a different candidate
// location entirely), so scoping the retract to only the touched path would
// leave the losing or former-winner file's stale edges behind (issue #5419
// P1: a union of both files' edges, or an empty graph when the new winner
// emits under a source_path the scoped retract never considered). A
// repository whose delta touched no CODEOWNERS candidate keeps the ordinary
// path-scoped retract, which stays a harmless no-op against
// DECLARES_CODEOWNER edges when the touched paths are not CODEOWNERS source
// paths.
func buildDeltaAwareRetractRows(
	repositoryIDs []string,
	filePathsByRepoID map[string][]string,
) []sharedintent.Row {
	rows := make([]sharedintent.Row, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		filePaths := filePathsByRepoID[repositoryID]
		if deltaTouchesCandidate(filePaths) {
			rows = append(rows, sharedintent.Row{RepositoryID: repositoryID})
			continue
		}
		rows = append(rows, sharedintent.Row{
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
