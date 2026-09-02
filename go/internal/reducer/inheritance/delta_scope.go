// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inheritance

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// DeltaScope is the per-repository delta retract scope one inheritance
// generation carries: which repositories reported a delta generation, and which
// repo-qualified file paths changed in each. Its fields are exported because
// the reducer root's cross-family sibling gates read them back to assert that
// every fenced repo-wide-retract domain scopes its retract the same way.
type DeltaScope struct {
	// RepositoryIDs are the repositories whose repository fact carried
	// delta_generation this cycle, sorted.
	RepositoryIDs []string
	// FilePathsByRepoID maps each such repository to its sorted, repo-qualified
	// changed file paths. A repository present in RepositoryIDs with no entry
	// here is on a delta generation whose paths could not be qualified.
	FilePathsByRepoID map[string][]string
	// HasDelta reports whether ANY repository in the scope is on a delta
	// generation. It is scope-wide and therefore never the per-repository gate;
	// see [sharedintent.ApplyRepoRefreshDeltaScope].
	HasDelta bool
}

func loadInheritanceMaterializationFacts(
	ctx context.Context,
	loader factload.FactLoader,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	_, hasKindLoader := loader.(factload.FactKindLoader)
	_, hasPayloadLoader := loader.(factload.FactPayloadValueLoader)
	if !hasKindLoader && !hasPayloadLoader {
		envelopes, err := loader.ListFacts(ctx, scopeID, generationID)
		if err != nil {
			return nil, factload.ClassifyFactLoadError(err)
		}
		return envelopes, nil
	}

	repositoryFacts, err := factload.LoadFactsForKinds(ctx, loader, scopeID, generationID, []string{factload.FactKindRepository})
	if err != nil {
		return nil, err
	}
	contentFacts, err := factload.LoadFactsForKindAndPayloadValue(
		ctx,
		loader,
		scopeID,
		generationID,
		factload.FactKindContentEntity,
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
	case factload.FactKindRepository:
		repositoryID := payloadcore.SemanticPayloadString(envelope.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = payloadcore.SemanticPayloadString(envelope.Payload, "graph_id")
		}
		if repositoryID != "" {
			return factload.FactKindRepository + ":" + repositoryID
		}
	case factload.FactKindContentEntity:
		entityID := payloadcore.SemanticPayloadString(envelope.Payload, "entity_id")
		if entityID != "" {
			return factload.FactKindContentEntity + ":" + entityID
		}
	}
	return ""
}

func BuildDeltaScope(envelopes []facts.Envelope) DeltaScope {
	seenRepoIDs := make(map[string]struct{})
	seenPathsByRepoID := make(map[string]map[string]struct{})
	scope := DeltaScope{}
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
		scope.HasDelta = true
		if _, ok := seenRepoIDs[repositoryID]; !ok {
			seenRepoIDs[repositoryID] = struct{}{}
			scope.RepositoryIDs = append(scope.RepositoryIDs, repositoryID)
		}
		repoPath := payloadcore.SemanticPayloadString(env.Payload, "path")
		if repoPath == "" {
			repoPath = payloadcore.SemanticPayloadString(env.Payload, "local_path")
		}
		for _, relativePath := range inheritanceDeltaRelativePaths(env.Payload) {
			filePath := payloadcore.QualifyDeltaPath(repoPath, relativePath)
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
	sort.Strings(scope.RepositoryIDs)
	if len(seenPathsByRepoID) == 0 {
		return scope
	}

	scope.FilePathsByRepoID = make(map[string][]string, len(seenPathsByRepoID))
	for repositoryID, seen := range seenPathsByRepoID {
		filePaths := make([]string, 0, len(seen))
		for filePath := range seen {
			filePaths = append(filePaths, filePath)
		}
		sort.Strings(filePaths)
		scope.FilePathsByRepoID[repositoryID] = filePaths
	}
	return scope
}

func inheritanceDeltaRelativePaths(payload map[string]any) []string {
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

// BuildRetractRows returns the retract scope rows for one inheritance
// generation: file-scoped rows on a delta generation, repo-wide rows otherwise.
// It is exported because the reducer root's cross-family retract-reachability
// gate drives every fenced domain through its own builder.
func BuildRetractRows(
	repositoryIDs []string,
	deltaScope DeltaScope,
) []sharedintent.Row {
	if len(repositoryIDs) == 0 {
		return nil
	}
	if deltaScope.HasDelta {
		return buildDeltaRetractRows(repositoryIDs, deltaScope.FilePathsByRepoID)
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

func buildDeltaRetractRows(
	repositoryIDs []string,
	filePathsByRepoID map[string][]string,
) []sharedintent.Row {
	rows := make([]sharedintent.Row, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		rows = append(rows, sharedintent.Row{
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
