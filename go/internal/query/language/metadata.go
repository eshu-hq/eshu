// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// EntitySearch aliases querycontract.LanguageEntitySearch. Package query's
// content_reader_entity_search.go names this type as languageEntitySearch
// through a forwarding alias in language_alias.go (#6642); this is the
// leaf's canonical spelling, exported at its declaration because that root
// file is the caller that needs it.
type EntitySearch = querycontract.LanguageEntitySearch

// languageEntityContentSearcher aliases
// querycontract.LanguageEntityContentSearcher. *ContentReader (package
// query) implements it; a store that does not gets the per-repository
// fallback in searchLanguageEntities below, which is bound but issues one
// statement per granted repository. package query's language_alias.go keeps
// the compile-time pin against *ContentReader, since ContentReader is a
// later lane's family and this leaf never names it.
type languageEntityContentSearcher = querycontract.LanguageEntityContentSearcher

// enrichLanguageResultsWithContentMetadata merges Postgres content-index
// metadata into graph-sourced results, keyed by repository plus file
// path/label/name/start line (languageResultRepositoryMatchKey). merged
// reports true whenever a matched row's content metadata was
// non-empty and was merged into that row via mergeGraphFirstMetadata -- every
// no-op path below (nil Content, unmapped label, zero content rows, no key
// match, or a matched key whose content metadata is empty) reports
// merged=false. This is not the same as "at least one content VALUE actually
// changed the row": mergeGraphFirstMetadata lets any non-nil graph-derived
// value in the row's existing metadata override the content value at the
// same key, so a row whose content metadata keys are all shadowed by
// non-nil graph values still reports merged=true even though the final
// metadata map is unchanged from the graph-only answer. The direction stays
// safe either way -- this can only over-claim toward a hybrid/derived truth
// basis when a plain graph read would have been equally accurate, never
// launder a content-served answer as authoritative-graph-only (#5761 P1-1).
func (h *Handler) enrichLanguageResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	language string,
	label string,
	query string,
	repoID string,
	limit int,
	grant codequery.LanguageQueryGrant,
) ([]map[string]any, bool, error) {
	if h == nil || h.Content == nil || len(results) == 0 {
		return results, false, nil
	}

	entityType := graphLabelToContentEntityType(label)
	if entityType == "" {
		return results, false, nil
	}

	for i := range results {
		entitysemantics.AttachSemanticSummary(results[i])
	}

	// #5167 batch 2a: this is a SECOND content read, issued after the graph
	// already answered. Left unbound it reads every tenant's rows to build the
	// merge-key map below, so a key collision would merge another tenant's
	// metadata into a granted row. The grant closes the cross-tenant half; the
	// repository in the key closes the within-grant half.
	rows, err := h.searchLanguageEntities(ctx, EntitySearch{
		RepoID:               repoID,
		Language:             language,
		EntityType:           entityType,
		Query:                query,
		Limit:                limit,
		AllowedRepositoryIDs: grant.AllowedRepositoryIDs,
	})
	if err != nil {
		return nil, false, fmt.Errorf("enrich language results with content metadata: %w", err)
	}
	if len(rows) == 0 {
		return results, false, nil
	}

	metadataByKey := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		metadataByKey[languageResultRepositoryMatchKey(
			row.RepoID,
			row.RelativePath,
			row.EntityType,
			row.EntityName,
			row.StartLine,
		)] = row.Metadata
	}

	merged := false
	for i := range results {
		key := languageResultRepositoryMatchKey(
			languageResultRepositoryID(results[i]),
			querycontract.StringVal(results[i], "file_path"),
			label,
			querycontract.StringVal(results[i], "name"),
			querycontract.IntVal(results[i], "start_line"),
		)
		metadata, ok := metadataByKey[key]
		if !ok || len(metadata) == 0 {
			continue
		}
		results[i]["metadata"] = mergeGraphFirstMetadata(results[i]["metadata"], metadata)
		entitysemantics.AttachSemanticSummary(results[i])
		merged = true
	}

	return results, merged, nil
}

// languageResultMatchKey identifies one entity by where it sits in a file. It
// is shared with the entity and code-search enrichments (root's
// entity_metadata.go, search_metadata.go), which anchor their own reads
// differently, so this route adds the repository through the wrapper below
// rather than changing the shared shape.
// languageResultMatchKey forwards to querycontract.LanguageResultMatchKey.
func languageResultMatchKey(filePath string, entityType string, name string, startLine int) string {
	return querycontract.LanguageResultMatchKey(filePath, entityType, name, startLine)
}

// languageResultRepositoryMatchKey is the merge key
// enrichLanguageResultsWithContentMetadata uses.
//
// repoID leads it because the other four components are not unique across
// repositories: a fork, a vendored copy, or a generated file two services both
// carry gives two repositories the same relative path, label, entity name and
// start line. Without the repository in the key those rows collided in
// metadataByKey, the last content row written won, and both graph rows were
// enriched from it. That is reachable inside ONE caller's own grant -- both
// repositories granted, the answer still wrong -- so the grant binding this
// route added does not cover it.
//
// An empty repoID is a key in its own right rather than a wildcard, so a row
// the graph could not attribute to a repository can only match a content row
// that carries none either, never borrow an attributed row's metadata.
func languageResultRepositoryMatchKey(repoID string, filePath string, entityType string, name string, startLine int) string {
	return repoID + "|" + languageResultMatchKey(filePath, entityType, name, startLine)
}

// languageResultRepositoryID reads the repository a graph-sourced result row
// belongs to. Every builder that reaches the enrichment projects `r.id as
// repo_id`; buildRepositoryCypher instead projects the repository's own id as
// `id`, so that is the fallback. A Repository-labelled read cannot reach the
// enrichment today -- graphLabelToContentEntityType maps that label to "" and
// enrichLanguageResultsWithContentMetadata returns before the merge -- so the
// fallback is there to keep the key correct if that mapping ever changes,
// rather than to serve a live path.
func languageResultRepositoryID(result map[string]any) string {
	if repoID := querycontract.StringVal(result, "repo_id"); repoID != "" {
		return repoID
	}
	return querycontract.StringVal(result, "id")
}

func mergeGraphFirstMetadata(existing any, fallback map[string]any) map[string]any {
	if len(fallback) == 0 {
		if metadata, ok := existing.(map[string]any); ok {
			return metadata
		}
		return nil
	}
	merged := make(map[string]any, len(fallback))
	for key, value := range fallback {
		merged[key] = value
	}
	if current, ok := existing.(map[string]any); ok {
		for key, value := range current {
			if value == nil {
				continue
			}
			merged[key] = value
		}
	}
	return merged
}

// searchLanguageEntities runs one content-store entity lookup with the grant
// bound.
//
// A store that satisfies languageEntityContentSearcher takes the grant into its
// own statement, so one read serves the whole granted set and the LIMIT page is
// taken from it. A store that does not -- the shape a test fake or an older
// implementation has -- can only be asked about one repository at a time, so a
// corpus-wide scoped search iterates the granted repositories rather than
// asking for repository "", which the unrestricted statement answers with every
// tenant's rows. That is the same fallback shape symbolNameFallbackEntities
// (package codequery) uses on POST /api/v0/code/symbols/search.
func (h *Handler) searchLanguageEntities(
	ctx context.Context,
	search EntitySearch,
) ([]querycontract.EntityContent, error) {
	if h == nil || h.Content == nil {
		return nil, fmt.Errorf("content reader is required for %s queries", search.EntityType)
	}
	if searcher, ok := h.Content.(languageEntityContentSearcher); ok {
		return searcher.SearchEntitiesByLanguageAndTypeForAccess(ctx, search)
	}
	if search.RepoID != "" || len(search.AllowedRepositoryIDs) == 0 {
		return h.Content.SearchEntitiesByLanguageAndType(
			ctx, search.RepoID, search.Language, search.EntityType, search.Query, search.Limit,
		)
	}
	entities := make([]querycontract.EntityContent, 0, search.Limit)
	for _, repoID := range search.AllowedRepositoryIDs {
		if len(entities) >= search.Limit {
			break
		}
		rows, err := h.Content.SearchEntitiesByLanguageAndType(
			ctx, repoID, search.Language, search.EntityType, search.Query, search.Limit-len(entities),
		)
		if err != nil {
			return nil, err
		}
		entities = append(entities, rows...)
	}
	return entities, nil
}

// queryContentByLanguage answers one dispatch branch entirely from the content
// store, with the caller's grant bound at the read.
func (h *Handler) queryContentByLanguage(
	ctx context.Context,
	language, entityType, query, repoID string,
	limit int,
	grant codequery.LanguageQueryGrant,
) ([]map[string]any, error) {
	rows, err := h.searchLanguageEntities(ctx, EntitySearch{
		RepoID:               repoID,
		Language:             language,
		EntityType:           entityType,
		Query:                query,
		Limit:                limit,
		AllowedRepositoryIDs: grant.AllowedRepositoryIDs,
	})
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result := map[string]any{
			"entity_id":  row.EntityID,
			"name":       row.EntityName,
			"labels":     []string{row.EntityType},
			"file_path":  row.RelativePath,
			"repo_id":    row.RepoID,
			"language":   row.Language,
			"start_line": row.StartLine,
			"end_line":   row.EndLine,
			"metadata":   row.Metadata,
		}
		entitysemantics.AttachSemanticSummary(result)
		results = append(results, result)
	}

	return results, nil
}
