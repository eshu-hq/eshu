// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func (h *CodeHandler) searchGlobalEntityNames(ctx context.Context, name, language string, limit int, exact bool) ([]map[string]any, error) {
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if access.Empty() {
		return []map[string]any{}, nil
	}
	searcher, ok := h.Content.(EntityNameSearcher)
	if !ok {
		return nil, querycontract.ErrEntityNameSearchUnavailable
	}
	search := EntityNameSearch{Name: name, Match: EntityNameMatchSubstring, Scope: EntityNameScopeAll, Limit: limit}
	if exact {
		search.Match = EntityNameMatchExact
	}
	if access.Scoped() {
		search.Scope = EntityNameScopeRepositories
		search.RepositoryIDs = access.RepositorySearchIDs()
	}
	if strings.TrimSpace(language) != "" {
		search.Languages = querycontract.NormalizedLanguageVariants(language)
	}
	rows, err := searcher.SearchEntityNames(ctx, search)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(rows))
	for _, entity := range rows {
		result := map[string]any{
			"entity_id": entity.EntityID, "entity_name": entity.EntityName, "entity_type": entity.EntityType,
			"name": entity.EntityName, "labels": []string{entity.EntityType}, "repo_name": entity.RepoName,
			"file_path": entity.RelativePath, "start_line": entity.StartLine, "end_line": entity.EndLine,
			"language": entity.Language, "metadata": entity.Metadata, "repo_id": entity.RepoID,
		}
		entitysemantics.AttachSemanticSummary(result)
		results = append(results, result)
	}
	return results, nil
}

// This file is the narrow surviving remnant of the #6060 lane-A P0 shim
// (formerly family_code_shim_story.go in root package query). Every other
// entry in that shim was deleted at the move and its call sites repointed
// to name codemodel/codeshaping/codeowners/querycontract directly (see the
// #6060 move commit). These five did not get that treatment: each is called
// from inside a function registered in
// internal/queryplan/grandfathered_non_hot.go with a source digest frozen
// from the `func` keyword through the closing brace -- callChainCandidateOneHopRows,
// relationshipsGraphRow, and transitiveRelationshipsGraphRow. Qualifying
// these calls (e.g. codemodel.GraphEntityIDPredicate(...)) would change
// those three functions' source text and break their pinned digest;
// transitiveRelationshipsGraphRow is one of the four grandfathered
// call-graph reads that cannot be honestly typed into any existing
// queryplan class (unbounded :CALLS reads, no LIMIT), so forcing its
// conversion here would mean inventing a dishonest bound rather than
// describing the read it actually performs. Keeping these five names
// resolving locally, unqualified, keeps those three functions' source
// byte-identical across the move. Do not delete an entry here without
// first re-deriving whether its caller is still grandfathered.

// relationshipsRequest aliases the leaf-owned lookup request so
// transitiveRelationshipsGraphRow's signature stays unchanged.
type relationshipsRequest = codemodel.RelationshipsRequest

// relationshipGraphRowCypher forwards to the leaf-owned row fragment so
// relationshipsGraphRow's call site stays unchanged.
func relationshipGraphRowCypher(predicate string) string {
	return codemodel.RelationshipGraphRowCypher(predicate)
}

// buildTransitiveRelationshipRowsCypher forwards to the leaf-owned
// traversal builder so transitiveRelationshipsGraphRow's call site stays
// unchanged.
func buildTransitiveRelationshipRowsCypher(
	entityID string,
	direction string,
	maxDepth int,
	backend GraphBackend,
) (string, map[string]any) {
	return codemodel.BuildTransitiveRelationshipRowsCypher(entityID, direction, maxDepth, backend)
}

// buildTransitiveRelationshipGraphResponse forwards to the leaf-owned
// traversal shaper so transitiveRelationshipsGraphRow's call site stays
// unchanged.
func buildTransitiveRelationshipGraphResponse(
	metadataRow map[string]any,
	rows []map[string]any,
	direction string,
) map[string]any {
	return codemodel.BuildTransitiveRelationshipGraphResponse(metadataRow, rows, direction)
}

// graphEntityIDPredicate forwards to the leaf-owned identity predicate so
// callChainCandidateOneHopRows and relationshipsGraphRow's call sites stay
// unchanged.
func graphEntityIDPredicate(alias string, param string) string {
	return codemodel.GraphEntityIDPredicate(alias, param)
}
